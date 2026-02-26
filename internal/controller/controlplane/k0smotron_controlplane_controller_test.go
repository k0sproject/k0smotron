//go:build !envtest

package controlplane

import (
	"testing"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cluster-api/util/conditions"
)

func TestIsClusterSpecSynced(t *testing.T) {
	testCases := []struct {
		name     string
		kmcSpec  kapi.ClusterSpec
		kcpSpec  kapi.ClusterSpec
		expected bool
	}{
		{
			name: "cluster spec is synced",
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
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := isClusterSpecSynced(tc.kmcSpec, tc.kcpSpec)
			require.NoError(t, err)
			require.EqualValues(t, tc.expected, result)
		})
	}
}

func TestComputeK0smotronClusterReconciliationStatus(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectMessage  bool
	}{
		{
			name:           "reconciliation successful",
			status:         kapi.ReconciliationStatusSuccessful,
			expectedStatus: metav1.ConditionTrue,
			expectedReason: cpv1beta1.ReconciliationSucceededReason,
		},
		{
			name:           "failed reconciling services",
			status:         kapi.ReconciliationStatusFailedPrefix + " reconciling services",
			expectedStatus: metav1.ConditionFalse,
			expectedReason: cpv1beta1.ReconciliationFailedReason,
			expectMessage:  true,
		},
		{
			name:           "failed reconciling etcd with details",
			status:         kapi.ReconciliationStatusFailedPrefix + " reconciling etcd, some error details",
			expectedStatus: metav1.ConditionFalse,
			expectedReason: cpv1beta1.ReconciliationFailedReason,
			expectMessage:  true,
		},
		{
			name:           "empty status (initial state)",
			status:         "",
			expectedStatus: metav1.ConditionFalse,
			expectedReason: cpv1beta1.ReconciliationInProgressReason,
		},
		{
			name:           "ErrNotReady string",
			status:         "not ready yet",
			expectedStatus: metav1.ConditionFalse,
			expectedReason: cpv1beta1.ReconciliationInProgressReason,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kcp := &cpv1beta1.K0smotronControlPlane{}
			computeK0smotronClusterReconciliationStatus(kcp, tt.status)

			cond := conditions.Get(kcp, cpv1beta1.K0smotronClusterReconciledCondition)
			require.NotNil(t, cond)
			require.Equal(t, tt.expectedStatus, cond.Status)
			require.Equal(t, tt.expectedReason, cond.Reason)
			if tt.expectMessage {
				require.Equal(t, tt.status, cond.Message)
			}
		})
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
