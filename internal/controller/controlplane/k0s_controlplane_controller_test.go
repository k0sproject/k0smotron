//go:build !envtest

/*
Copyright 2023.

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

package controlplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv1beta2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
)

func TestK0sConfigEnrichment(t *testing.T) {
	var testCases = []struct {
		cluster *clusterv1.Cluster
		kcp     *cpv1beta2.K0sControlPlane
		want    *unstructured.Unstructured
	}{
		{
			cluster: &clusterv1.Cluster{},
			kcp:     &cpv1beta2.K0sControlPlane{},
			want:    nil,
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: clusterv1.ClusterNetwork{
						Services: clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.96.0.0/12"},
						},
						Pods: clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.244.0.0/16"},
						},
					},
				},
			},
			kcp: &cpv1beta2.K0sControlPlane{},
			want: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]any{
					"network": map[string]any{"serviceCIDR": "10.96.0.0/12", "podCIDR": "10.244.0.0/16"},
				},
			}},
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: clusterv1.ClusterNetwork{
						Services: clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.96.0.0/12"},
						},
						Pods: clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.244.0.0/16"},
						},
					},
				},
			},
			kcp: &cpv1beta2.K0sControlPlane{
				Spec: cpv1beta2.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv1beta2.K0sConfigSpec{
						K0s: &unstructured.Unstructured{Object: map[string]any{
							"spec": map[string]any{
								"network": map[string]any{"serviceCIDR": "10.98.0.0/12"},
							},
						}},
					},
				},
			},
			want: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]any{
					"network": map[string]any{"serviceCIDR": "10.98.0.0/12", "podCIDR": "10.244.0.0/16"},
				},
			}},
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: clusterv1.ClusterNetwork{
						ServiceDomain: "cluster.local",
					},
				},
			},
			kcp: &cpv1beta2.K0sControlPlane{},
			want: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]any{
					"network": map[string]any{"clusterDomain": "cluster.local"},
				},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			actual, err := enrichK0sConfigWithClusterData(tc.cluster, tc.kcp.Spec.K0sConfigSpec.K0s)
			require.NoError(t, err)
			require.Equal(t, tc.want, actual)
		})
	}
}

func TestClusterToK0sControlPlane(t *testing.T) {
	newCluster := func(kind, name string) *clusterv1.Cluster {
		return &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"},
			Spec: clusterv1.ClusterSpec{
				ControlPlaneRef: clusterv1.ContractVersionedObjectReference{Kind: kind, Name: name},
			},
		}
	}

	for _, tc := range []struct {
		name string
		obj  client.Object
		want []ctrl.Request
	}{
		{
			name: "a k0s control plane is enqueued in the cluster namespace",
			obj:  newCluster("K0sControlPlane", "cp"),
			want: []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: "ns", Name: "cp"}}},
		},
		{
			name: "the hosted control plane flavor is left to its own controller",
			obj:  newCluster("K0smotronControlPlane", "cp"),
		},
		{
			name: "a cluster with no control plane reference is skipped",
			obj:  newCluster("K0sControlPlane", ""),
		},
		{
			name: "anything that is not a cluster is skipped",
			obj:  &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "ns"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clusterToK0sControlPlane(context.Background(), tc.obj))
		})
	}
}

// certScheme is the minimum needed to store certificate secrets.
func certScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	return scheme
}

func certScope(initialized bool) *controlplane {
	return &controlplane{
		cluster: &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}},
		kcp: &cpv1beta2.K0sControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", UID: "kcp-uid"},
			Status: cpv1beta2.K0sControlPlaneStatus{
				Initialization: cpv1beta2.Initialization{
					ControlPlaneInitialized: new(initialized),
				},
			},
		},
	}
}

// TestEnsureCertificatesDoesNotRegenerateAfterInit covers a deleted certificate
// authority being reported rather than quietly replaced with a new one.
func TestEnsureCertificatesDoesNotRegenerateAfterInit(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(certScheme(t)).Build()
	c := &K0sController{Client: cl, SecretCachingClient: cl}

	// Generated on the way up, which is the path that has to keep working.
	require.NoError(t, c.ensureCertificates(context.Background(), certScope(false)))

	stored := &corev1.SecretList{}
	require.NoError(t, cl.List(context.Background(), stored))
	require.Len(t, stored.Items, 4, "all four authorities are internal, so all four are minted")

	// Up now, so the same call must not mint anything.
	require.NoError(t, c.ensureCertificates(context.Background(), certScope(true)))

	ca := &corev1.Secret{}
	caKey := client.ObjectKey{Namespace: "default", Name: "test-ca"}
	require.NoError(t, cl.Get(context.Background(), caKey, ca))
	require.NoError(t, cl.Delete(context.Background(), ca))

	err := c.ensureCertificates(context.Background(), certScope(true))
	require.ErrorContains(t, err, "are missing and are not regenerated")
	require.ErrorContains(t, err, "certificates ca are missing", "the error has to name the purpose that is gone")

	require.True(t, apierrors.IsNotFound(cl.Get(context.Background(), caKey, &corev1.Secret{})),
		"a new authority here would invalidate every node certificate in the cluster")
}

// TestEnsureCertificatesGeneratesBeforeInit covers the pre init path still filling a
// gap, since nothing is using the certificates yet.
func TestEnsureCertificatesGeneratesBeforeInit(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(certScheme(t)).Build()
	c := &K0sController{Client: cl, SecretCachingClient: cl}

	require.NoError(t, c.ensureCertificates(context.Background(), certScope(false)))

	ca := &corev1.Secret{}
	caKey := client.ObjectKey{Namespace: "default", Name: "test-ca"}
	require.NoError(t, cl.Get(context.Background(), caKey, ca))
	require.NoError(t, cl.Delete(context.Background(), ca))

	require.NoError(t, c.ensureCertificates(context.Background(), certScope(false)))
	require.NoError(t, cl.Get(context.Background(), caKey, ca), "an unused authority is still generated")
}
