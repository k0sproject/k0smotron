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
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateStatefulSet_PodTemplate_ProbeOverride(t *testing.T) {
	tests := []struct {
		name    string
		cluster *km.Cluster
		want    func(t *testing.T, sts *corev1.PodTemplateSpec)
	}{
		{
			name: "Default probes without podTemplate",
			cluster: &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: km.ClusterSpec{
					Replicas: 1,
				},
			},
			want: func(t *testing.T, podTemplate *corev1.PodTemplateSpec) {
				controllerContainer := findContainer(podTemplate.Spec.Containers, "controller")
				assert.NotNil(t, controllerContainer)
				assert.NotNil(t, controllerContainer.ReadinessProbe)
				assert.Equal(t, int32(60), controllerContainer.ReadinessProbe.InitialDelaySeconds)
				assert.NotNil(t, controllerContainer.LivenessProbe)
				assert.Equal(t, int32(90), controllerContainer.LivenessProbe.InitialDelaySeconds)
			},
		},
		{
			name: "PodTemplate overriding readinessProbe initialDelaySeconds",
			cluster: &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: km.ClusterSpec{
					Replicas: 1,
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									ReadinessProbe: &corev1.Probe{
										InitialDelaySeconds: 120,
										PeriodSeconds:       10,
										FailureThreshold:    15,
										ProbeHandler: corev1.ProbeHandler{
											Exec: &corev1.ExecAction{
												Command: []string{"k0s", "status"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: func(t *testing.T, podTemplate *corev1.PodTemplateSpec) {
				controllerContainer := findContainer(podTemplate.Spec.Containers, "controller")
				assert.NotNil(t, controllerContainer)
				assert.NotNil(t, controllerContainer.ReadinessProbe)
				assert.Equal(t, int32(120), controllerContainer.ReadinessProbe.InitialDelaySeconds)
				// LivenessProbe should still have default value
				assert.NotNil(t, controllerContainer.LivenessProbe)
				assert.Equal(t, int32(90), controllerContainer.LivenessProbe.InitialDelaySeconds)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fake client for testing
			scheme := runtime.NewScheme()
			require.NoError(t, km.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.cluster).
				Build()

			// Create a minimal scope for testing
			scope := &kmcScope{
				client: client,
				clusterSettings: clusterSettings{
					serviceCIDR:         "10.96.0.0/12",
					kubernetesServiceIP: "10.96.0.1",
					clusterDomain:       "cluster.local",
				},
			}
			sts, err := scope.generateStatefulSet(tc.cluster)
			assert.NoError(t, err)

			// Calculate hash after all modifications
			hash := controller.ComputeHash(&sts.Spec.Template, sts.Status.CollisionCount)

			// Verify the hash is stored in annotations
			assert.Equal(t, hash, sts.Annotations[statefulSetAnnotation])

			// Run test-specific assertions
			tc.want(t, &sts.Spec.Template)
		})
	}
}

func TestGenerateStatefulSet_PodTemplate_HashChanges(t *testing.T) {
	baseCluster := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-base",
			Namespace: "default",
		},
		Spec: km.ClusterSpec{
			Replicas: 1,
		},
	}

	clusterWithProbeOverride := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-override",
			Namespace: "default",
		},
		Spec: km.ClusterSpec{
			Replicas: 1,
			PodTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "controller",
							ReadinessProbe: &corev1.Probe{
								InitialDelaySeconds: 120,
								PeriodSeconds:       10,
								FailureThreshold:    15,
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"k0s", "status"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Create a fake client for testing
	scheme := runtime.NewScheme()
	require.NoError(t, km.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	client1 := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(baseCluster).
		Build()

	client2 := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterWithProbeOverride).
		Build()

	scope1 := &kmcScope{
		client: client1,
		clusterSettings: clusterSettings{
			serviceCIDR:         "10.96.0.0/12",
			kubernetesServiceIP: "10.96.0.1",
			clusterDomain:       "cluster.local",
		},
	}
	sts1, err := scope1.generateStatefulSet(baseCluster)
	assert.NoError(t, err)
	hash1 := sts1.Annotations[statefulSetAnnotation]

	scope2 := &kmcScope{
		client: client2,
		clusterSettings: clusterSettings{
			serviceCIDR:         "10.96.0.0/12",
			kubernetesServiceIP: "10.96.0.1",
			clusterDomain:       "cluster.local",
		},
	}
	sts2, err := scope2.generateStatefulSet(clusterWithProbeOverride)
	assert.NoError(t, err)
	hash2 := sts2.Annotations[statefulSetAnnotation]

	// Hash should change when probe settings change
	assert.NotEqual(t, hash1, hash2, "Hash should change when probe settings are overridden via podTemplate")
}
