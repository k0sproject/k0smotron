//go:build !envtest

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

package certs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func newFakeClientWithWatch(objs ...runtime.Object) client.WithWatch {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return fake.NewFakeClient(objs...)
}

func TestSaveRenewed_updatesDataPreservingMetadata(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "kmc-etcd-server",
			Namespace:   "default",
			Labels:      map[string]string{"cluster.x-k8s.io/cluster-name": "kmc"},
			Annotations: map[string]string{"cert.k0smotron.io/renewed": "true"},
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "k0smotron.io/v1beta2", Kind: "Cluster", Name: "kmc", UID: "abc"},
			},
		},
		Data: map[string][]byte{"tls.crt": []byte("old-crt"), "tls.key": []byte("old-key")},
	}
	c := newFakeClient(existing)

	err := SaveRenewed(context.Background(), c,
		client.ObjectKey{Namespace: "default", Name: "kmc-etcd-server"},
		[]byte("new-crt"), []byte("new-key"))
	require.NoError(t, err)

	got := &corev1.Secret{}
	require.NoError(t, c.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "kmc-etcd-server"}, got))

	assert.Equal(t, []byte("new-crt"), got.Data["tls.crt"])
	assert.Equal(t, []byte("new-key"), got.Data["tls.key"])
	assert.Equal(t, "kmc", got.Labels["cluster.x-k8s.io/cluster-name"])
	assert.Equal(t, "true", got.Annotations["cert.k0smotron.io/renewed"])
	assert.Equal(t, existing.OwnerReferences, got.OwnerReferences)
}

func TestSaveRenewed_missingSecret(t *testing.T) {
	c := newFakeClient()

	err := SaveRenewed(context.Background(), c,
		client.ObjectKey{Namespace: "default", Name: "nope"},
		[]byte("crt"), []byte("key"))

	assert.Error(t, err)
}

func TestSaveRenewed_retriesOnConflict(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kmc-etcd-server",
			Namespace: "default",
		},
		Data: map[string][]byte{"tls.crt": []byte("old-crt"), "tls.key": []byte("old-key")},
	}
	fakeClient := newFakeClientWithWatch(existing)

	updateCount := 0
	c := interceptor.NewClient(fakeClient, interceptor.Funcs{
		Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			updateCount++
			if updateCount == 1 {
				// Return conflict on first attempt to trigger retry
				return apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, "kmc-etcd-server", errors.New("conflict"))
			}
			// Delegate to real client on retry
			return client.Update(ctx, obj, opts...)
		},
	})

	err := SaveRenewed(context.Background(), c,
		client.ObjectKey{Namespace: "default", Name: "kmc-etcd-server"},
		[]byte("new-crt"), []byte("new-key"))
	require.NoError(t, err)

	// Verify Update was called twice (first conflict, then success)
	assert.Equal(t, 2, updateCount)

	// Verify final secret has the NEW data (proves retry re-read and re-applied)
	got := &corev1.Secret{}
	require.NoError(t, fakeClient.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "kmc-etcd-server"}, got))
	assert.Equal(t, []byte("new-crt"), got.Data["tls.crt"])
	assert.Equal(t, []byte("new-key"), got.Data["tls.key"])
}
