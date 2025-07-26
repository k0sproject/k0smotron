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

	"github.com/stretchr/testify/require"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEtcd_resourceRequirements(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
	}{
		{
			name:    "Default - No resources specified",
			cluster: &km.Cluster{}, // No Resources specified
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
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := new(ClusterReconciler)
			sts := r.generateEtcdStatefulSet(tc.cluster, nil, 1)
			resources := sts.Spec.Template.Spec.Containers[0].Resources

			switch tc.name {
			case "Default - No resources specified":
				require.Empty(t, resources.Requests)
				require.Empty(t, resources.Limits)
			case "Resources specified - Requests only":
				require.Equal(t, resource.MustParse("100m"), *resources.Requests.Cpu())
				require.Equal(t, resource.MustParse("128Mi"), *resources.Requests.Memory())
				require.Empty(t, resources.Limits)
			case "Resources specified - Requests and limits":
				require.Equal(t, resource.MustParse("100m"), *resources.Requests.Cpu())
				require.Equal(t, resource.MustParse("128Mi"), *resources.Requests.Memory())
				require.Equal(t, resource.MustParse("200m"), *resources.Limits.Cpu())
				require.Equal(t, resource.MustParse("256Mi"), *resources.Limits.Memory())
			}
		})
	}
}

func TestEtcd_schedulingConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
	}{
		{
			name:    "Default - No scheduling configuration",
			cluster: &km.Cluster{},
		},
		{
			name: "RuntimeClass specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						RuntimeClass: ptr.To("kata-containers"),
					},
				},
			},
		},
		{
			name: "Tolerations specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						Tolerations: []corev1.Toleration{
							{
								Key:      "etcd",
								Operator: corev1.TolerationOpEqual,
								Value:    "true",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
		},
		{
			name: "TopologySpreadConstraints specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
							{
								MaxSkew:           1,
								TopologyKey:       "topology.kubernetes.io/zone",
								WhenUnsatisfiable: corev1.DoNotSchedule,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "etcd"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "NodeSelector specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						NodeSelector: map[string]string{
							"node-type": "etcd",
							"zone":      "us-west-1a",
						},
					},
				},
			},
		},
		{
			name: "Custom Affinity specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						Affinity: &corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "node-type",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"etcd"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "PriorityClassName specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						PriorityClassName: "high-priority",
					},
				},
			},
		},
		{
			name: "All scheduling fields specified",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						RuntimeClass: ptr.To("kata-containers"),
						Tolerations: []corev1.Toleration{
							{
								Key:      "etcd",
								Operator: corev1.TolerationOpEqual,
								Value:    "true",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
						TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
							{
								MaxSkew:           1,
								TopologyKey:       "topology.kubernetes.io/zone",
								WhenUnsatisfiable: corev1.DoNotSchedule,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "etcd"},
								},
							},
						},
						NodeSelector: map[string]string{
							"node-type": "etcd",
						},
						Affinity: &corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "node-type",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"etcd"},
												},
											},
										},
									},
								},
							},
						},
						PriorityClassName: "high-priority",
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := new(ClusterReconciler)
			sts := r.generateEtcdStatefulSet(tc.cluster, nil, 1)
			podSpec := &sts.Spec.Template.Spec

			switch tc.name {
			case "Default - No scheduling configuration":
				require.Nil(t, podSpec.RuntimeClassName)
				require.Nil(t, podSpec.Tolerations)
				require.Nil(t, podSpec.NodeSelector)
				require.Empty(t, podSpec.PriorityClassName)
				// Default anti-affinity should be present
				require.NotNil(t, podSpec.Affinity)
				require.NotNil(t, podSpec.Affinity.PodAntiAffinity)
			case "RuntimeClass specified":
				require.NotNil(t, podSpec.RuntimeClassName)
				require.Equal(t, "kata-containers", *podSpec.RuntimeClassName)
			case "Tolerations specified":
				require.NotNil(t, podSpec.Tolerations)
				require.Len(t, podSpec.Tolerations, 1)
				require.Equal(t, "etcd", podSpec.Tolerations[0].Key)
				require.Equal(t, corev1.TolerationOpEqual, podSpec.Tolerations[0].Operator)
				require.Equal(t, "true", podSpec.Tolerations[0].Value)
				require.Equal(t, corev1.TaintEffectNoSchedule, podSpec.Tolerations[0].Effect)
			case "TopologySpreadConstraints specified":
				require.NotNil(t, podSpec.TopologySpreadConstraints)
				require.Len(t, podSpec.TopologySpreadConstraints, 1)
				require.Equal(t, int32(1), podSpec.TopologySpreadConstraints[0].MaxSkew)
				require.Equal(t, "topology.kubernetes.io/zone", podSpec.TopologySpreadConstraints[0].TopologyKey)
				require.Equal(t, corev1.DoNotSchedule, podSpec.TopologySpreadConstraints[0].WhenUnsatisfiable)
			case "NodeSelector specified":
				require.NotNil(t, podSpec.NodeSelector)
				require.Equal(t, "etcd", podSpec.NodeSelector["node-type"])
				require.Equal(t, "us-west-1a", podSpec.NodeSelector["zone"])
			case "Custom Affinity specified":
				require.NotNil(t, podSpec.Affinity)
				require.NotNil(t, podSpec.Affinity.NodeAffinity)
				require.NotNil(t, podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
				// Custom affinity should override default PodAntiAffinity
				require.Nil(t, podSpec.Affinity.PodAntiAffinity)
			case "PriorityClassName specified":
				require.Equal(t, "high-priority", podSpec.PriorityClassName)
			case "All scheduling fields specified":
				require.NotNil(t, podSpec.RuntimeClassName)
				require.Equal(t, "kata-containers", *podSpec.RuntimeClassName)
				require.NotNil(t, podSpec.Tolerations)
				require.Len(t, podSpec.Tolerations, 1)
				require.NotNil(t, podSpec.TopologySpreadConstraints)
				require.Len(t, podSpec.TopologySpreadConstraints, 1)
				require.NotNil(t, podSpec.NodeSelector)
				require.Equal(t, "etcd", podSpec.NodeSelector["node-type"])
				require.NotNil(t, podSpec.Affinity)
				require.NotNil(t, podSpec.Affinity.NodeAffinity)
				require.Equal(t, "high-priority", podSpec.PriorityClassName)
			}
		})
	}
}

func TestEtcd_imagePullConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
	}{
		{
			name:    "Default - No ImagePullSecrets",
			cluster: &km.Cluster{},
		},
		{
			name: "ImagePullSecrets specified - single secret",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						ImagePullSecrets: []corev1.LocalObjectReference{
							{Name: "regcred"},
						},
					},
				},
			},
		},
		{
			name: "ImagePullSecrets specified - multiple secrets",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					Etcd: km.EtcdSpec{
						ImagePullSecrets: []corev1.LocalObjectReference{
							{Name: "regcred"},
							{Name: "another-secret"},
							{Name: "third-secret"},
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := new(ClusterReconciler)
			sts := r.generateEtcdStatefulSet(tc.cluster, nil, 1)
			podSpec := &sts.Spec.Template.Spec

			switch tc.name {
			case "Default - No ImagePullSecrets":
				require.Nil(t, podSpec.ImagePullSecrets)
			case "ImagePullSecrets specified - single secret":
				require.NotNil(t, podSpec.ImagePullSecrets)
				require.Len(t, podSpec.ImagePullSecrets, 1)
				require.Equal(t, "regcred", podSpec.ImagePullSecrets[0].Name)
			case "ImagePullSecrets specified - multiple secrets":
				require.NotNil(t, podSpec.ImagePullSecrets)
				require.Len(t, podSpec.ImagePullSecrets, 3)
				require.Equal(t, "regcred", podSpec.ImagePullSecrets[0].Name)
				require.Equal(t, "another-secret", podSpec.ImagePullSecrets[1].Name)
				require.Equal(t, "third-secret", podSpec.ImagePullSecrets[2].Name)
			}
		})
	}
}

func TestEtcd_topologySpreadConstraintsFallback(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
	}{
		{
			name: "Etcd TopologySpreadConstraints takes precedence over cluster-level",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           2,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "cluster"},
							},
						},
					},
					Etcd: km.EtcdSpec{
						TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
							{
								MaxSkew:           1,
								TopologyKey:       "topology.kubernetes.io/zone",
								WhenUnsatisfiable: corev1.DoNotSchedule,
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "etcd"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Fallback to cluster-level TopologySpreadConstraints when etcd-specific not set",
			cluster: &km.Cluster{
				Spec: km.ClusterSpec{
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           2,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "cluster"},
							},
						},
					},
					Etcd: km.EtcdSpec{
						// No etcd-specific TopologySpreadConstraints
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := new(ClusterReconciler)
			sts := r.generateEtcdStatefulSet(tc.cluster, nil, 1)
			podSpec := &sts.Spec.Template.Spec

			switch tc.name {
			case "Etcd TopologySpreadConstraints takes precedence over cluster-level":
				require.NotNil(t, podSpec.TopologySpreadConstraints)
				require.Len(t, podSpec.TopologySpreadConstraints, 1)
				// Should use etcd-specific constraints, not cluster-level
				require.Equal(t, int32(1), podSpec.TopologySpreadConstraints[0].MaxSkew)
				require.Equal(t, "topology.kubernetes.io/zone", podSpec.TopologySpreadConstraints[0].TopologyKey)
			case "Fallback to cluster-level TopologySpreadConstraints when etcd-specific not set":
				require.NotNil(t, podSpec.TopologySpreadConstraints)
				require.Len(t, podSpec.TopologySpreadConstraints, 1)
				// Should use cluster-level constraints
				require.Equal(t, int32(2), podSpec.TopologySpreadConstraints[0].MaxSkew)
				require.Equal(t, "kubernetes.io/hostname", podSpec.TopologySpreadConstraints[0].TopologyKey)
			}
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
			r := new(ClusterReconciler)
			sts := r.generateEtcdStatefulSet(tc.cluster, nil, 1)
			for _, w := range tc.want {
				require.True(t, strings.Contains(sts.Spec.Template.Spec.Containers[0].Args[1], w))
			}
		})
	}
}
