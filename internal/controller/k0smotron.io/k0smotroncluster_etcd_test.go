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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestEtcd_calculateDesiredReplicas(t *testing.T) {
	var tests = []struct {
		cluster *km.Cluster
		want    int32
	}{
		{cluster: &km.Cluster{}, want: 1},
		{cluster: &km.Cluster{Spec: km.ClusterSpec{Replicas: 1}}, want: 1},
		{cluster: &km.Cluster{Spec: km.ClusterSpec{Replicas: 2}}, want: 3},
		{cluster: &km.Cluster{Spec: km.ClusterSpec{Replicas: 3}}, want: 3},
		{cluster: &km.Cluster{Spec: km.ClusterSpec{Replicas: 4}}, want: 5},
		{cluster: &km.Cluster{Spec: km.ClusterSpec{Replicas: 5}}, want: 5},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			got := calculateDesiredReplicas(tc.cluster, nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEtcd_resourceRequirements(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
		want    func(t *testing.T, resources corev1.ResourceRequirements)
	}{
		{
			name:    "Default - No resources specified",
			cluster: &km.Cluster{}, // No Resources specified
			want: func(t *testing.T, resources corev1.ResourceRequirements) {
				assert.Empty(t, resources.Requests)
				assert.Empty(t, resources.Limits)
			},
		},
		{
			name: "Resources specified - Requests only",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			},
			want: func(t *testing.T, resources corev1.ResourceRequirements) {
				assert.Equal(t, resource.MustParse("100m"), *resources.Requests.Cpu())
				assert.Equal(t, resource.MustParse("128Mi"), *resources.Requests.Memory())
				assert.Empty(t, resources.Limits)
			},
		},
		{
			name: "Resources specified - Requests and limits",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
			want: func(t *testing.T, resources corev1.ResourceRequirements) {
				assert.Equal(t, resource.MustParse("100m"), *resources.Requests.Cpu())
				assert.Equal(t, resource.MustParse("128Mi"), *resources.Requests.Memory())
				assert.Equal(t, resource.MustParse("200m"), *resources.Limits.Cpu())
				assert.Equal(t, resource.MustParse("256Mi"), *resources.Limits.Memory())
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sts, err := generateEtcdStatefulSet(tc.cluster, nil, 1)
			assert.NoError(t, err)
			resources := sts.Spec.Template.Spec.Containers[0].Resources
			tc.want(t, resources)
		})
	}
}

func TestEtcd_generateEtcdStatefulSet(t *testing.T) {
	var tests = []struct {
		cluster *km.Cluster
		want    []string
	}{
		{
			cluster: &km.Cluster{},
			want: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=5m",
				"--snapshot-count=10000",
			}},
		{
			cluster: &km.Cluster{Spec: km.ClusterSpec{Etcd: km.EtcdSpec{Args: []string{
				"--auto-compaction-mode=periodic",
			}}}},
			want: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=5m",
				"--snapshot-count=10000",
			}},
		{
			cluster: &km.Cluster{Spec: km.ClusterSpec{Etcd: km.EtcdSpec{Args: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=2h",
				"--snapshot-count=50000",
			}}}},
			want: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=2h",
				"--snapshot-count=50000",
			}},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			sts, err := generateEtcdStatefulSet(tc.cluster, nil, 1)
			assert.NoError(t, err)
			for _, w := range tc.want {
				assert.True(t, strings.Contains(sts.Spec.Template.Spec.Containers[0].Args[1], w))
			}
		})
	}
}

func TestEtcd_podTemplateMerge(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
		want    func(t *testing.T, sts *km.Cluster)
	}{
		{
			name: "PodTemplate with nodeSelector",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						PodTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								NodeSelector: map[string]string{
									"node-type": "etcd",
								},
							},
						},
					},
				},
			},
			want: func(t *testing.T, cluster *km.Cluster) {
				sts, err := generateEtcdStatefulSet(cluster, nil, 1)
				assert.NoError(t, err)
				assert.Equal(t, "etcd", sts.Spec.Template.Spec.NodeSelector["node-type"])
			},
		},
		{
			name: "PodTemplate with tolerations",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						PodTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Tolerations: []corev1.Toleration{
									{
										Key:      "node-role.kubernetes.io/etcd",
										Operator: corev1.TolerationOpExists,
										Effect:   corev1.TaintEffectNoSchedule,
									},
								},
							},
						},
					},
				},
			},
			want: func(t *testing.T, cluster *km.Cluster) {
				sts, err := generateEtcdStatefulSet(cluster, nil, 1)
				assert.NoError(t, err)
				assert.Len(t, sts.Spec.Template.Spec.Tolerations, 1)
				assert.Equal(t, "node-role.kubernetes.io/etcd", sts.Spec.Template.Spec.Tolerations[0].Key)
			},
		},
		{
			name: "PodTemplate with sidecar container",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						PodTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "sidecar",
										Image: "sidecar:latest",
									},
								},
							},
						},
					},
				},
			},
			want: func(t *testing.T, cluster *km.Cluster) {
				sts, err := generateEtcdStatefulSet(cluster, nil, 1)
				assert.NoError(t, err)
				// Should have both etcd container and sidecar
				assert.Len(t, sts.Spec.Template.Spec.Containers, 2)
				containerNames := make(map[string]bool)
				for _, c := range sts.Spec.Template.Spec.Containers {
					containerNames[c.Name] = true
				}
				assert.True(t, containerNames["etcd"], "etcd container should exist")
				assert.True(t, containerNames["sidecar"], "sidecar container should exist")
			},
		},
		{
			name: "PodTemplate overriding etcd container resources",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						PodTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "etcd",
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("200m"),
												corev1.ResourceMemory: resource.MustParse("256Mi"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: func(t *testing.T, cluster *km.Cluster) {
				sts, err := generateEtcdStatefulSet(cluster, nil, 1)
				assert.NoError(t, err)
				etcdContainer := findContainer(sts.Spec.Template.Spec.Containers, "etcd")
				assert.NotNil(t, etcdContainer)
				// PodTemplate should override the Resources field from EtcdSpec.Resources
				assert.Equal(t, resource.MustParse("200m"), *etcdContainer.Resources.Requests.Cpu())
				assert.Equal(t, resource.MustParse("256Mi"), *etcdContainer.Resources.Requests.Memory())
			},
		},
		{
			name: "PodTemplate with priorityClassName",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						PodTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								PriorityClassName: "high-priority",
							},
						},
					},
				},
			},
			want: func(t *testing.T, cluster *km.Cluster) {
				sts, err := generateEtcdStatefulSet(cluster, nil, 1)
				assert.NoError(t, err)
				assert.Equal(t, "high-priority", sts.Spec.Template.Spec.PriorityClassName)
			},
		},
		{
			name: "PodTemplate with runtimeClassName",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						PodTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RuntimeClassName: stringPtr("gvisor"),
							},
						},
					},
				},
			},
			want: func(t *testing.T, cluster *km.Cluster) {
				sts, err := generateEtcdStatefulSet(cluster, nil, 1)
				assert.NoError(t, err)
				assert.NotNil(t, sts.Spec.Template.Spec.RuntimeClassName)
				assert.Equal(t, "gvisor", *sts.Spec.Template.Spec.RuntimeClassName)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.want(t, tc.cluster)
		})
	}
}

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func stringPtr(s string) *string {
	return &s
}
