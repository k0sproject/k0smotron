package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestFindNodeAddress(t *testing.T) {

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
			got := FindNodeAddress(tt.nodes)
			assert.Equal(t, tt.want, got)
		})
	}
}
