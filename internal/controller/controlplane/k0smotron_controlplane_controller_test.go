package controlplane

import (
	"testing"

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestFormatStatusVersion tests the formatStatusVersion function
func TestFormatStatusVersion(t *testing.T) {
	tests := []struct {
		name           string
		specVersion    string
		statusVersion  string
		expectedStatus string
		description    string
	}{
		{
			name:           "spec without k0s suffix",
			specVersion:    "v1.33.1",
			statusVersion:  "v1.33.1-k0s.0",
			expectedStatus: "v1.33.1",
			description:    "When spec.version doesn't have -k0s suffix, status.version should also not have it",
		},
		{
			name:           "spec with k0s suffix",
			specVersion:    "v1.33.1-k0s.0",
			statusVersion:  "v1.33.1-k0s.0",
			expectedStatus: "v1.33.1-k0s.0",
			description:    "When spec.version has -k0s suffix, status.version should keep it",
		},
		{
			name:           "spec with custom k0s suffix",
			specVersion:    "v1.33.1-k0s.1",
			statusVersion:  "v1.33.1-k0s.1",
			expectedStatus: "v1.33.1-k0s.1",
			description:    "When spec.version has custom -k0s suffix, status.version should keep it",
		},
		{
			name:           "spec without suffix, status with different suffix",
			specVersion:    "v1.33.1",
			statusVersion:  "v1.33.1-k0s.1",
			expectedStatus: "v1.33.1",
			description:    "When spec.version doesn't have -k0s suffix, status.version should remove it even if it's different",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the actual FormatStatusVersion function
			result := FormatStatusVersion(tt.specVersion, tt.statusVersion)
			assert.Equal(t, tt.expectedStatus, result, tt.description)
		})
	}
}

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
					Object: map[string]interface{}{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]interface{}{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]any{
							"api": map[string]interface{}{
								"externalAddress": "172.18.0.2",
								"port":            int64(30443),
								"sans": []interface{}{
									"custom.sans.domain",
									"10.96.138.152",
									"172.18.0.2",
									"kmc-docker-test-nodeport",
									"kmc-docker-test-nodeport.default",
									"kmc-docker-test-nodeport.default.svc",
									"kmc-docker-test-nodeport.default.svc.cluster.local",
								},
							},
							"konnectivity": map[string]interface{}{
								"agentPort": int64(30132),
							},
							"network": map[string]interface{}{
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
					Object: map[string]interface{}{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]interface{}{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]interface{}{
							"api": map[string]interface{}{
								"externalAddress": "172.18.0.2",
								"port":            int64(30443),
								"sans": []interface{}{
									"custom.sans.domain",
								},
							},
							"network": map[string]interface{}{
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
					Object: map[string]interface{}{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]interface{}{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]any{
							"api": map[string]interface{}{
								"externalAddress": "172.18.0.2",
								"port":            int64(30443),
								"sans": []interface{}{
									"10.96.138.152",
									"172.18.0.2",
									"kmc-docker-test-nodeport",
									"kmc-docker-test-nodeport.default",
									"kmc-docker-test-nodeport.default.svc",
									"kmc-docker-test-nodeport.default.svc.cluster.local",
								},
							},
							"konnectivity": map[string]interface{}{
								"agentPort": int64(30132),
							},
							"network": map[string]interface{}{
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
					Object: map[string]interface{}{
						"apiVersion": "k0s.k0sproject.io/v1beta1",
						"kind":       "ClusterConfig",
						"metadata": map[string]interface{}{
							"name":      "k0s",
							"namespace": "kube-system",
						},
						"spec": map[string]interface{}{
							"network": map[string]interface{}{
								"clusterDomain": "cluster.local",
								"podCIDR":       "192.168.0.0/16",
								"serviceCIDR":   "10.128.0.0/12",
							},
							// kmcspec does not have same value for api.externalAddress
							"api": map[string]interface{}{
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
