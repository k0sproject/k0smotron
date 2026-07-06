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
	"context"
	"fmt"
	"testing"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	kapi "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/version"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestListControlPlanePods_IsNamespaceScoped(t *testing.T) {
	const clusterName = "test"

	// Two clusters share the same name in different namespaces, each with its own
	// control plane pods. Counting must not leak pods across namespaces.
	newPod := func(name, namespace string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"cluster.x-k8s.io/cluster-name":  clusterName,
					"cluster.x-k8s.io/control-plane": "true",
				},
			},
		}
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			newPod("foo-cp-0", "ns-foo"),
			newPod("foo-cp-1", "ns-foo"),
			newPod("foo-cp-2", "ns-foo"),
			newPod("bar-cp-0", "ns-bar"),
			newPod("bar-cp-1", "ns-bar"),
			newPod("bar-cp-2", "ns-bar"),
		).
		Build()

	for _, ns := range []string{"ns-foo", "ns-bar"} {
		t.Run(ns, func(t *testing.T) {
			cluster := &clusterv1.Cluster{
				ObjectMeta: v1.ObjectMeta{Name: clusterName, Namespace: ns},
			}
			pods, err := listControlPlanePods(context.Background(), cl, cluster)
			require.NoError(t, err)
			require.Len(t, pods.Items, 3, "control plane pod count must be scoped to the cluster namespace")
			for _, pod := range pods.Items {
				require.Equal(t, ns, pod.Namespace)
			}
		})
	}
}

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

// Test_computeAvailability regression-guards k0rdent/kcm#2884
func Test_computeAvailability(t *testing.T) {
	const (
		clusterName = "hosted"
		clusterNs   = "default"
	)

	// syntactically valid kubeconfig pointing to an unroutable endpoint; the fallback only builds a client
	// from this blob, the subsequent ping is expected to fail and that is exactly what distinguishes
	// fallback success from a pre-ping ClusterClientCreationFailed
	kubeconfigYAML := []byte(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:1
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: dummy
`)

	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      secret.Name(clusterName, secret.Kubeconfig),
			Namespace: clusterNs,
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: kubeconfigYAML,
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))

	tests := []struct {
		name          string
		hubObjects    []client.Object
		wantCondition string
	}{
		{
			name:          "fallback client used when ClusterCache is not connected and kubeconfig secret exists",
			hubObjects:    []client.Object{kubeconfigSecret},
			wantCondition: "KubeSystemNamespaceNotAccessible",
		},
		{
			name:          "ClusterClientCreationFailed surfaced when kubeconfig secret is missing",
			hubObjects:    nil,
			wantCondition: "ClusterClientCreationFailed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &clusterv1.Cluster{
				ObjectMeta: v1.ObjectMeta{Name: clusterName, Namespace: clusterNs},
			}

			hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.hubObjects...).Build()

			// non-matching cluster key -> clustercache.ErrClusterNotConnected on GetClient (kcm#2884)
			disconnectedCache := clustercache.NewFakeClusterCache(
				fake.NewClientBuilder().Build(),
				client.ObjectKey{Name: "unrelated", Namespace: "unrelated"},
			)

			c := &K0smotronController{
				Client:       hubClient,
				ClusterCache: disconnectedCache,
				Scheme:       scheme,
			}
			kcp := &cpv1beta2.K0smotronControlPlane{
				ObjectMeta: v1.ObjectMeta{Name: clusterName, Namespace: clusterNs},
			}

			c.computeAvailability(context.Background(), cluster, kcp)

			cond := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
			require.NotNil(t, cond, "Available condition must be set")
			require.Equal(t, tt.wantCondition, cond.Reason)
		})
	}
}
