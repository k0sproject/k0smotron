package util

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	clusterinventoryv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
)

func TestGetKmcClientFromClusterProfile_ClusterProfileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterinventoryv1alpha1.AddToScheme(scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	ref := &kapi.ClusterProfileRef{
		Name:      "nonexistent",
		Namespace: "default",
	}

	_, _, _, err := GetKmcClientFromClusterProfile(context.Background(), c, ref)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get ClusterProfile")
}

func TestGetKmcClientFromClusterProfile_NoCredentialProvidersConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterinventoryv1alpha1.AddToScheme(scheme))

	cp := &clusterinventoryv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cp", Namespace: "default"},
		Spec:       clusterinventoryv1alpha1.ClusterProfileSpec{},
		Status: clusterinventoryv1alpha1.ClusterProfileStatus{
			AccessProviders: []clusterinventoryv1alpha1.AccessProvider{
				{Name: "kubeconfig", Cluster: clientcmdv1.Cluster{Server: "https://1.2.3.4:6443"}},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(cp).
		WithObjects(cp).
		Build()

	cp.Status = clusterinventoryv1alpha1.ClusterProfileStatus{
		AccessProviders: []clusterinventoryv1alpha1.AccessProvider{
			{Name: "kubeconfig", Cluster: clientcmdv1.Cluster{Server: "https://1.2.3.4:6443"}},
		},
	}
	require.NoError(t, c.Status().Update(context.Background(), cp))

	// Ensure CredentialProvidersConfigPath is empty
	originalPath := CredentialProvidersConfigPath
	CredentialProvidersConfigPath = ""
	defer func() { CredentialProvidersConfigPath = originalPath }()

	ref := &kapi.ClusterProfileRef{
		Name:      "test-cp",
		Namespace: "default",
	}

	_, _, _, err := GetKmcClientFromClusterProfile(context.Background(), c, ref)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access providers config path is not set")
}
