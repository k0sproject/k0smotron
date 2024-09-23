package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
)

func TestK0sConfigEnrichment(t *testing.T) {
	var testCases = []struct {
		cluster *clusterv1.Cluster
		kcp     *v1beta1.K0sControlPlane
		want    *unstructured.Unstructured
	}{
		{
			cluster: &clusterv1.Cluster{},
			kcp:     &v1beta1.K0sControlPlane{},
			want:    nil,
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						Services: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.96.0.0/12"},
						},
						Pods: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.244.0.0/16"},
						},
					},
				},
			},
			kcp: &v1beta1.K0sControlPlane{},
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"network": map[string]interface{}{"serviceCIDR": "10.96.0.0/12", "podCIDR": "10.244.0.0/16"},
				},
			}},
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						Services: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.96.0.0/12"},
						},
						Pods: &clusterv1.NetworkRanges{
							CIDRBlocks: []string{"10.244.0.0/16"},
						},
					},
				},
			},
			kcp: &v1beta1.K0sControlPlane{
				Spec: v1beta1.K0sControlPlaneSpec{
					K0sConfigSpec: bootstrapv1.K0sConfigSpec{
						K0s: &unstructured.Unstructured{Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"network": map[string]interface{}{"serviceCIDR": "10.98.0.0/12"},
							},
						}},
					},
				},
			},
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"network": map[string]interface{}{"serviceCIDR": "10.98.0.0/12", "podCIDR": "10.244.0.0/16"},
				},
			}},
		},
		{
			cluster: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						ServiceDomain: "cluster.local",
					},
				},
			},
			kcp: &v1beta1.K0sControlPlane{},
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "k0s.k0sproject.io/v1beta1",
				"kind":       "ClusterConfig",
				"spec": map[string]interface{}{
					"network": map[string]interface{}{"clusterDomain": "cluster.local"},
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
