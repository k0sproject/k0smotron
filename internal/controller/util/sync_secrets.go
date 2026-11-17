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

// SyncReferencedSecrets mirrors Secrets referenced by kmc (mounts and kine datasource) from src
// to dst, creating or updating as needed. Synced Secrets are owned by externalOwner for GC on deletion.
func SyncReferencedSecrets(ctx context.Context, src, dst client.Client, kmc *km.Cluster, externalOwner client.Object) error {
	// name -> whether the Secret is optional; a Secret is optional only if every reference to it is.
	optional := map[string]bool{}
	add := func(name string, isOptional bool) {
		if prev, ok := optional[name]; ok {
			isOptional = isOptional && prev
		}
		optional[name] = isOptional
	}

	for _, mount := range kmc.Spec.Mounts {
		if mount.Secret != nil && mount.Secret.SecretName != "" {
			add(mount.Secret.SecretName, mount.Secret.Optional != nil && *mount.Secret.Optional)
		}
	}
	if name := kmc.Spec.Storage.Kine.DataSourceSecretName; name != "" {
		add(name, false)
	}

	for _, name := range slices.Sorted(maps.Keys(optional)) {
		srcSecret := &corev1.Secret{}
		if err := src.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: name}, srcSecret); err != nil {
			if apierrors.IsNotFound(err) && optional[name] {
				continue
			}
			return fmt.Errorf("failed to get secret %s/%s from the management cluster: %w", kmc.Namespace, name, err)
		}

		if err := syncSecret(ctx, dst, srcSecret, kmc, externalOwner); err != nil {
			return fmt.Errorf("failed to sync secret %s/%s to the remote host cluster: %w", kmc.Namespace, name, err)
		}
	}

	return nil
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
	// The management cluster is the source of truth for metadata, so whatever is on an existing copy is replaced.
	dstSecret.Labels = maps.Clone(srcSecret.Labels)
	dstSecret.Annotations = maps.Clone(srcSecret.Annotations)
	// dst.Scheme() must have corev1 registered, which is the case for all manager-managed clients.
	if err := SetExternalOwnerReference(kmc, dstSecret, dst.Scheme(), externalOwner); err != nil {
		return err
	}

	if !exists {
		return dst.Create(ctx, dstSecret)
	}
	return dst.Update(ctx, dstSecret)
}
