//go:build !envtest

/*
Copyright 2026.

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
	"fmt"
	"testing"

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/version"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIsClusterSpecSynced(t *testing.T) {
	const (
		withAnnotation    = "with annotation"
		withoutAnnotation = "without annotation"
	)

	kmc := kapi.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				AnnotationKeyClusterSpecHash: "86c6b5f8df",
			},
		},
	}
	testCases := []struct {
		name     string
		kmcSpec  kapi.ClusterSpec
		kcpSpec  kapi.ClusterSpec
		expected bool
	}{
		{
			name: "cluster spec is synced",
			kmcSpec: kapi.ClusterSpec{
				Manifests: []corev1.Volume{
					{
						Name: "manifest-1",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "manifest-configmap-1",
								},
							},
						},
					},
					// Simulate the manifest volume created by the controller.
					{
						Name: kmc.GetEndpointConfigMapName(),
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: kmc.GetEndpointConfigMapName(),
								},
							},
						},
					},
				},
				CertificateRefs: []kapi.CertificateRef{
					{
						Type: "cert-type-1",
						Name: "cert-name-1",
					},
					{
						Type: "cert-type-2",
						Name: "cert-name-2",
					},
					{
						Type: "cert-type-3",
						Name: "cert-name-3",
					},
				},
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]any{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]any{
							"api": map[string]any{
								"externalAddress": "172.18.0.2",
								"port":            int64(30443),
								"sans": []any{
									"custom.sans.domain",
									"10.96.138.152",
									"172.18.0.2",
									"kmc-docker-test-nodeport",
									"kmc-docker-test-nodeport.default",
									"kmc-docker-test-nodeport.default.svc",
									"kmc-docker-test-nodeport.default.svc.cluster.local",
								},
							},
							"konnectivity": map[string]any{
								"agentPort": int64(30132),
							},
							"network": map[string]any{
								"clusterDomain": "cluster.local",
								"podCIDR":       "192.168.0.0/16",
								"serviceCIDR":   "10.128.0.0/12",
							},
						},
					},
				},
			},
			kcpSpec: kapi.ClusterSpec{
				CertificateRefs: []kapi.CertificateRef{
					{
						Type: "cert-type-1",
						Name: "cert-name-1",
					},
					{
						Type: "cert-type-2",
						Name: "cert-name-2",
					},
				},
				Manifests: []corev1.Volume{
					{
						Name: "manifest-1",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "manifest-configmap-1",
								},
							},
						},
					},
				},
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]any{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]any{
							"api": map[string]any{
								"externalAddress": "172.18.0.2",
								"port":            int64(30443),
								"sans": []any{
									"custom.sans.domain",
								},
							},
							"network": map[string]any{
								"clusterDomain": "cluster.local",
								"podCIDR":       "192.168.0.0/16",
								"serviceCIDR":   "10.128.0.0/12",
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "cluster spec is not synced",
			kmcSpec: kapi.ClusterSpec{
				CertificateRefs: []kapi.CertificateRef{
					{
						Type: "cert-type-1",
						Name: "cert-name-1",
					},
					{
						Type: "cert-type-2",
						Name: "cert-name-2",
					},
					{
						Type: "cert-type-3",
						Name: "cert-name-3",
					},
				},
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]any{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]any{
							"api": map[string]any{
								"externalAddress": "172.18.0.2",
								"port":            int64(30443),
								"sans": []any{
									"10.96.138.152",
									"172.18.0.2",
									"kmc-docker-test-nodeport",
									"kmc-docker-test-nodeport.default",
									"kmc-docker-test-nodeport.default.svc",
									"kmc-docker-test-nodeport.default.svc.cluster.local",
								},
							},
							"konnectivity": map[string]any{
								"agentPort": int64(30132),
							},
							"network": map[string]any{
								"clusterDomain": "cluster.local",
								"podCIDR":       "192.168.0.0/16",
								"serviceCIDR":   "10.128.0.0/12",
							},
						},
					},
				},
			},
			kcpSpec: kapi.ClusterSpec{
				CertificateRefs: []kapi.CertificateRef{
					{
						Type: "cert-type-1",
						Name: "cert-name-1",
					},
					{
						Type: "cert-type-2",
						Name: "cert-name-2",
					},
				},
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]any{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]any{
							"network": map[string]any{
								"clusterDomain": "cluster.local",
								"podCIDR":       "192.168.0.0/16",
								"serviceCIDR":   "10.128.0.0/12",
							},
							// kmcspec does not have same value for api.externalAddress
							"api": map[string]any{
								"externalAddress": "172.18.0.3",
							},
						},
					},
				},
			},
			expected: false,
		},
	}
	for _, mode := range []string{withAnnotation, withoutAnnotation} {
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				if mode == withoutAnnotation {
					delete(kmc.GetAnnotations(), AnnotationKeyClusterSpecHash)
				}
				tc.name = fmt.Sprintf("%s - %s", tc.name, mode)
				kmc.Spec = tc.kmcSpec
				result, kcpSpecHash, err := isClusterSpecSynced(kmc, tc.kcpSpec)
				require.NoError(t, err)
				require.EqualValues(t, tc.expected, result)
				if mode == withAnnotation {
					require.EqualValues(t, tc.expected, kcpSpecHash == kmc.GetAnnotations()[AnnotationKeyClusterSpecHash])
				}
			})
		}
	}

}

func Test_alignToSpecVersionFormat(t *testing.T) {
	tests := []struct {
		name           string
		specVersion    *version.Version
		currentVersion *version.Version
		want           *version.Version
	}{
		{
			name:           "both versions have same format",
			specVersion:    version.MustParse("v1.33.1-k0s.0"),
			currentVersion: version.MustParse("v1.33.1-k0s.1"),
			want:           version.MustParse("v1.33.1-k0s.1"),
		},
		{
			name:           "versions does not have same format: spec with +k0s, current with -k0s",
			specVersion:    version.MustParse("v1.33.1+k0s.0"),
			currentVersion: version.MustParse("v1.33.1-k0s.1"),
			want:           version.MustParse("v1.33.1+k0s.1"),
		},
		{
			name:           "versions does not have same format: spec with -k0s, current with +k0s",
			specVersion:    version.MustParse("v1.33.1-k0s.0"),
			currentVersion: version.MustParse("v1.33.1+k0s.1"),
			want:           version.MustParse("v1.33.1-k0s.1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := alignToSpecVersionFormat(tt.specVersion, tt.currentVersion)
			require.NoError(t, err)
			require.True(t, tt.want.Equal(got), "alignToSpecVersionFormat() = %v, want %v", got, tt.want)
		})
	}
}
