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

	kapi "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/k0sproject/version"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/contract"
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

// Test_patchInfrastructureStatus regression-guards k0rdent/kcm#2884: the k0smotron-managed
// Infrastructure must be marked provisioned off the control-plane endpoint being available, not off
// ControlPlaneInitialized. The latter requires a successful API ping through ClusterCache, which
// itself won't connect until Infrastructure is provisioned -> permanent deadlock.
// It also asserts the provisioned bit is written to the contract-correct field: status.ready for the
// v1beta1 contract, status.initialization.provisioned for v1beta2.
func Test_patchInfrastructureStatus(t *testing.T) {
	const (
		clusterName = "hosted"
		clusterNs   = "default"
		infraGroup  = "infrastructure.cluster.x-k8s.io"
		infraKind   = "GenericInfrastructureCluster"
	)

	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	validEndpoint := clusterv1.APIEndpoint{Host: "api.example.com", Port: 6443}

	tests := []struct {
		name      string
		apiVer    string   // API version the InfrastructureCluster CRD serves
		wantPath  []string // status field CAPI reads for this version
		endpoint  clusterv1.APIEndpoint
		wantReady bool
	}{
		{
			name:      "v1beta1, endpoint provisioned -> status.ready true",
			apiVer:    "v1beta1",
			wantPath:  []string{"status", "ready"},
			endpoint:  validEndpoint,
			wantReady: true,
		},
		{
			name:      "v1beta1, endpoint not provisioned -> status.ready false",
			apiVer:    "v1beta1",
			wantPath:  []string{"status", "ready"},
			endpoint:  clusterv1.APIEndpoint{},
			wantReady: false,
		},
		{
			name:      "v1beta2, endpoint provisioned -> status.initialization.provisioned true",
			apiVer:    "v1beta2",
			wantPath:  []string{"status", "initialization", "provisioned"},
			endpoint:  validEndpoint,
			wantReady: true,
		},
		{
			name:      "v1beta2, endpoint not provisioned -> status.initialization.provisioned false",
			apiVer:    "v1beta2",
			wantPath:  []string{"status", "initialization", "provisioned"},
			endpoint:  clusterv1.APIEndpoint{},
			wantReady: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// CRD contract label maps the contract version to the served apiVersions; GetObjectFromContractVersionedRef
			// resolves the InfrastructureRef to that served version and stamps it onto the returned object.
			crd := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: v1.ObjectMeta{
					Name:   contract.CalculateCRDName(infraGroup, infraKind),
					Labels: map[string]string{"cluster.x-k8s.io/" + tt.apiVer: tt.apiVer},
				},
			}

			infra := &unstructured.Unstructured{}
			infra.SetAPIVersion(infraGroup + "/" + tt.apiVer)
			infra.SetKind(infraKind)
			infra.SetName(clusterName)
			infra.SetNamespace(clusterNs)
			infra.SetAnnotations(map[string]string{AnnotationKeyManagedBy: AnnotationValueManagedByK0smotron})

			fc := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(crd, infra).
				WithStatusSubresource(infra).
				Build()

			c := &K0smotronController{Client: fc, Scheme: scheme}
			cluster := &clusterv1.Cluster{
				ObjectMeta: v1.ObjectMeta{Name: clusterName, Namespace: clusterNs},
				Spec: clusterv1.ClusterSpec{
					ControlPlaneEndpoint: tt.endpoint,
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infraGroup,
						Kind:     infraKind,
						Name:     clusterName,
					},
				},
			}

			require.NoError(t, c.patchInfrastructureStatus(context.Background(), cluster))

			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(infra.GroupVersionKind())
			require.NoError(t, fc.Get(context.Background(), client.ObjectKeyFromObject(infra), got))
			ready, _, err := unstructured.NestedBool(got.Object, tt.wantPath...)
			require.NoError(t, err)
			require.Equal(t, tt.wantReady, ready)
		})
	}
}
