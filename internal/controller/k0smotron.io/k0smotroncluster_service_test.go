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

package k0smotronio

import (
	"testing"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterReconciler_findNodeAddress(t *testing.T) {

	tests := []struct {
		name  string
		nodes *v1.NodeList
		want  string
	}{
		{
			name: "when only internal is set",
			nodes: &v1.NodeList{
				Items: []v1.Node{
					{
						Status: v1.NodeStatus{
							Addresses: []v1.NodeAddress{
								{
									Type:    v1.NodeInternalIP,
									Address: "1.2.3.4",
								},
							},
						},
					},
				},
			},
			want: "1.2.3.4",
		},
		{
			name: "when only external is set",
			nodes: &v1.NodeList{
				Items: []v1.Node{
					{
						Status: v1.NodeStatus{
							Addresses: []v1.NodeAddress{
								{
									Type:    v1.NodeExternalIP,
									Address: "1.2.3.4",
								},
							},
						},
					},
				},
			},
			want: "1.2.3.4",
		},
		{
			name: "when both are set",
			nodes: &v1.NodeList{
				Items: []v1.Node{
					{
						Status: v1.NodeStatus{
							Addresses: []v1.NodeAddress{
								{
									Type:    v1.NodeExternalIP,
									Address: "1.1.1.1",
								},
								{
									Type:    v1.NodeInternalIP,
									Address: "2.2.2.2",
								},
							},
						},
					},
				},
			},
			want: "1.1.1.1",
		},
		{
			name: "when multiple addresses are set",
			nodes: &v1.NodeList{
				Items: []v1.Node{
					{
						Status: v1.NodeStatus{
							Addresses: []v1.NodeAddress{
								{
									Type:    v1.NodeInternalIP,
									Address: "2.2.2.2",
								},
								{
									Type:    v1.NodeExternalIP,
									Address: "1.1.1.1",
								},
								{
									Type:    v1.NodeInternalIP,
									Address: "3.3.3.3",
								},
								{
									Type:    v1.NodeExternalIP,
									Address: "4.4.4.4",
								},
							},
						},
					},
				},
			},
			want: "1.1.1.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterReconciler{}
			got := r.findNodeAddress(tt.nodes)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClusterReconciler_serviceAnnotations(t *testing.T) {
	tests := []struct {
		name string
		kmc  *km.Cluster
		want map[string]string
	}{
		{
			name: "when no annotations are set on either Cluster on svc",
			kmc: &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: km.ClusterSpec{},
			},
			want: map[string]string{},
		},
		{
			name: "when annotations are set on only Cluster",
			kmc: &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"test": "test",
					},
				},
				Spec: km.ClusterSpec{},
			},
			want: map[string]string{
				"test": "test",
			},
		},
		{
			name: "when annotations are set on both Cluster and svc",
			kmc: &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"test": "test",
					},
				},
				Spec: km.ClusterSpec{
					Service: km.ServiceSpec{
						Annotations: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
			want: map[string]string{
				"test": "test",
				"foo":  "bar",
			},
		},
		{
			name: "when same annotation is set on both Cluster and svc the svc annotation wins",
			kmc: &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"test": "test",
					},
				},
				Spec: km.ClusterSpec{
					Service: km.ServiceSpec{
						Annotations: map[string]string{
							"test": "foobar",
						},
					},
				},
			},
			want: map[string]string{
				"test": "foobar",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ClusterReconciler{}
			svc := r.generateService(tt.kmc)
			got := svc.Annotations
			assert.Equal(t, tt.want, got)
		})
	}
}
