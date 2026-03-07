package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/require"

	v2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCluster_ConvertFrom_WithClusterProfileRef(t *testing.T) {
	// ClusterProfileRef is dropped when converting from v1beta2.
	src := &v2.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v2.ClusterSpec{
			ClusterProfileRef: &v2.ClusterProfileRef{
				Name:      "my-profile",
				Namespace: "kube-system",
			},
		},
	}

	dst := &Cluster{}
	err := dst.ConvertFrom(src)
	require.NoError(t, err)
	// ClusterProfileRef has no v1beta1 equivalent, so KubeconfigRef remains nil
	require.Nil(t, dst.Spec.KubeconfigRef)
}

func TestCluster_ConvertTo_WithKubeconfigRef(t *testing.T) {
	src := &Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: ClusterSpec{
			KubeconfigRef: &v2.KubeconfigRef{
				Name:      "my-secret",
				Namespace: "default",
				Key:       "value",
			},
		},
	}

	dst := &v2.Cluster{}
	err := src.ConvertTo(dst)
	require.NoError(t, err)
	require.NotNil(t, dst.Spec.KubeconfigRef)
	require.Nil(t, dst.Spec.ClusterProfileRef)
}

func TestCluster_ConvertRoundTrip_KubeconfigRef(t *testing.T) {
	original := &Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: ClusterSpec{
			KubeconfigRef: &v2.KubeconfigRef{
				Name:      "roundtrip-secret",
				Namespace: "test-ns",
				Key:       "kubeconfig",
			},
			Replicas: 3,
		},
	}

	// v1beta1 -> v1beta2
	hub := &v2.Cluster{}
	err := original.ConvertTo(hub)
	require.NoError(t, err)

	// v1beta2 -> v1beta1
	result := &Cluster{}
	err = result.ConvertFrom(hub)
	require.NoError(t, err)

	require.Equal(t, original.Spec.KubeconfigRef, result.Spec.KubeconfigRef)
	require.Equal(t, original.Spec.Replicas, result.Spec.Replicas)
}
