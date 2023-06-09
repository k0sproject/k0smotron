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

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
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
