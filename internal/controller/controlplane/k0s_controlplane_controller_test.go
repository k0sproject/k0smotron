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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
