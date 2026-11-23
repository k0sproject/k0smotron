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
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type referencedObject struct {
	client.Object
	optional bool
}

// referencedNames returns the names of the Secrets and ConfigMaps referenced by the Cluster, each mapped
// to whether every reference to it is optional.
func referencedNames(kmc *km.Cluster) (secrets, configMaps map[string]bool) {
	secrets, configMaps = map[string]bool{}, map[string]bool{}

	// An object is optional only if every reference to it is.
	add := func(names map[string]bool, name string, optional bool) {
		if prev, ok := names[name]; ok {
			optional = optional && prev
		}
		names[name] = optional
	}
	addVolumeSource := func(vs corev1.VolumeSource) {
		if vs.Secret != nil && vs.Secret.SecretName != "" {
			add(secrets, vs.Secret.SecretName, vs.Secret.Optional != nil && *vs.Secret.Optional)
		}
		if vs.ConfigMap != nil && vs.ConfigMap.Name != "" {
			add(configMaps, vs.ConfigMap.Name, vs.ConfigMap.Optional != nil && *vs.ConfigMap.Optional)
		}
	}

	for _, mount := range kmc.Spec.Mounts {
		addVolumeSource(mount.VolumeSource)
	}
	for _, manifest := range kmc.Spec.Manifests {
		addVolumeSource(manifest.VolumeSource)
	}
	if name := kmc.Spec.Storage.Kine.DataSourceSecretName; name != "" {
		add(secrets, name, false)
	}

	// The endpoint ConfigMap is created directly in the hosting cluster by reconcileEndpointConfigMap;
	// it does not exist in the management cluster and must not be synced.
	delete(configMaps, kmc.GetEndpointConfigMapName())

	return secrets, configMaps
}

// A missing optional object is skipped, any other failure is returned.
func fetchReferencedObjects(ctx context.Context, src client.Client, kmc *km.Cluster) ([]referencedObject, error) {
	secrets, configMaps := referencedNames(kmc)

	kinds := []struct {
		kind  string
		names map[string]bool
		new   func() client.Object
	}{
		{"secret", secrets, func() client.Object { return &corev1.Secret{} }},
		{"configmap", configMaps, func() client.Object { return &corev1.ConfigMap{} }},
	}

	var refs []referencedObject
	for _, k := range kinds {
		for _, name := range slices.Sorted(maps.Keys(k.names)) {
			obj := k.new()
			if err := src.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: name}, obj); err != nil {
				if apierrors.IsNotFound(err) && k.names[name] {
					continue
				}
				return nil, fmt.Errorf("failed to get %s %s/%s from the management cluster: %w", k.kind, kmc.Namespace, name, err)
			}
			refs = append(refs, referencedObject{Object: obj, optional: k.names[name]})
		}
	}

	return refs, nil
}

// SyncReferencedObjects mirrors the Secrets and ConfigMaps referenced by kmc (mounts, manifests, certificate
// references and kine datasource) from src to dst, creating or updating as needed. It does nothing unless
// spec.remoteHostCluster.syncReferencedObjects is enabled. Synced objects are owned by externalOwner
// for GC on deletion.
func SyncReferencedObjects(ctx context.Context, src, dst client.Client, kmc *km.Cluster, externalOwner client.Object) error {
	if kmc.Spec.RemoteHostCluster == nil || !kmc.Spec.RemoteHostCluster.SyncReferencedObjects {
		return nil
	}

	refs, err := fetchReferencedObjects(ctx, src, kmc)
	if err != nil {
		return err
	}

	for _, ref := range refs {
		gvk, err := apiutil.GVKForObject(ref.Object, dst.Scheme())
		if err != nil {
			return err
		}
		runtimeObj, err := dst.Scheme().New(gvk)
		if err != nil {
			return err
		}
		// The object passed to CreateOrUpdate is overwritten by the Get, so it starts empty and is
		// filled from the fetched source object in the mutate function.
		obj := runtimeObj.(client.Object)
		obj.SetName(ref.GetName())
		obj.SetNamespace(ref.GetNamespace())

		_, err = controllerutil.CreateOrUpdate(ctx, dst, obj, func() error {
			if err := copyContent(obj, ref.Object); err != nil {
				return err
			}
			obj.SetLabels(maps.Clone(ref.GetLabels()))
			obj.SetAnnotations(maps.Clone(ref.GetAnnotations()))
			return SetExternalOwnerReference(kmc, obj, dst.Scheme(), externalOwner)
		})
		if err != nil {
			return fmt.Errorf("failed to sync %s %s/%s to the remote host cluster: %w", gvk.Kind, ref.GetNamespace(), ref.GetName(), err)
		}
	}

	return nil
}

func copyContent(dst, src client.Object) error {
	switch s := src.(type) {
	case *corev1.Secret:
		d := dst.(*corev1.Secret)
		if d.Type == "" {
			d.Type = s.Type
		}
		d.Data = maps.Clone(s.Data)
	case *corev1.ConfigMap:
		d := dst.(*corev1.ConfigMap)
		d.Data = maps.Clone(s.Data)
		d.BinaryData = maps.Clone(s.BinaryData)
	default:
		return fmt.Errorf("unsupported referenced object type %T", src)
	}
	return nil
}
