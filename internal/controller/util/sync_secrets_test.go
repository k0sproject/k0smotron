package util

import (
	"context"
	"testing"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestSyncReferencedSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	secret := func(name string, data string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
			Data:       map[string][]byte{"key": []byte(data)},
		}
	}
	secretMount := func(name string, optional bool) km.Mount {
		return km.Mount{
			Path:         "/mnt/" + name,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name, Optional: &optional}},
		}
	}
	newKmc := func(mounts []km.Mount, kineSecret string) *km.Cluster {
		kmc := &km.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "ns"}}
		kmc.Spec.Mounts = mounts
		kmc.Spec.Storage.Kine.DataSourceSecretName = kineSecret
		return kmc
	}

	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kmc-root-owner", Namespace: "ns", UID: "owner-uid"}}

	t.Run("creates mount and kine secrets, ignores other volume types", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret("mounted", "a"), secret("kine", "b"), secret("unreferenced", "c")).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).Build()
		kmc := newKmc([]km.Mount{
			secretMount("mounted", false),
			{Path: "/cm", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}},
		}, "kine")

		require.NoError(t, SyncReferencedSecrets(context.Background(), src, dst, kmc, owner))

		got := &corev1.Secret{}
		require.NoError(t, dst.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "mounted"}, got))
		require.Equal(t, []byte("a"), got.Data["key"])
		require.Len(t, got.OwnerReferences, 1)
		require.Equal(t, owner.UID, got.OwnerReferences[0].UID)
		require.NoError(t, dst.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "kine"}, got))
		require.Equal(t, []byte("b"), got.Data["key"])
		require.Len(t, got.OwnerReferences, 1)
		require.Equal(t, owner.UID, got.OwnerReferences[0].UID)
		err := dst.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "unreferenced"}, got)
		require.True(t, apierrors.IsNotFound(err))
	})

	t.Run("updates data of an existing secret", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret("kine", "new")).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret("kine", "old")).Build()

		require.NoError(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc(nil, "kine"), owner))

		got := &corev1.Secret{}
		require.NoError(t, dst.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "kine"}, got))
		require.Equal(t, []byte("new"), got.Data["key"])
		require.Len(t, got.OwnerReferences, 1)
		require.Equal(t, owner.UID, got.OwnerReferences[0].UID)
	})

	t.Run("copies labels and annotations, replacing those on an existing copy", func(t *testing.T) {
		srcSecret := secret("kine", "v")
		srcSecret.Labels = map[string]string{"from": "src"}
		srcSecret.Annotations = map[string]string{"note": "src"}
		dstSecret := secret("kine", "v")
		dstSecret.Labels = map[string]string{"stale": "label"}
		dstSecret.Annotations = map[string]string{"stale": "annotation"}
		src := fake.NewClientBuilder().WithScheme(scheme).WithObjects(srcSecret).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dstSecret).Build()

		require.NoError(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc(nil, "kine"), owner))

		got := &corev1.Secret{}
		require.NoError(t, dst.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "kine"}, got))
		require.Equal(t, map[string]string{"from": "src"}, got.Labels)
		require.Equal(t, map[string]string{"note": "src"}, got.Annotations)
	})

	t.Run("secret referenced twice is synced once", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret("shared", "x")).Build()
		creates := 0
		dst := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				creates++
				return c.Create(ctx, obj, opts...)
			},
		}).Build()

		require.NoError(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc([]km.Mount{secretMount("shared", false)}, "shared"), owner))
		require.Equal(t, 1, creates)
	})

	t.Run("secret referenced as optional and required is required", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).Build()

		require.Error(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc([]km.Mount{secretMount("shared", true)}, "shared"), owner))
	})

	t.Run("missing required secret fails", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).Build()

		require.Error(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc(nil, "kine"), owner))
	})

	t.Run("missing optional secret is skipped", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).Build()

		require.NoError(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc([]km.Mount{secretMount("opt", true)}, ""), owner))
	})

	t.Run("no references is a no-op", func(t *testing.T) {
		src := fake.NewClientBuilder().WithScheme(scheme).Build()
		dst := fake.NewClientBuilder().WithScheme(scheme).Build()

		require.NoError(t, SyncReferencedSecrets(context.Background(), src, dst, newKmc(nil, ""), owner))
	})
}
