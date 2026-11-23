//go:build !envtest

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
	"github.com/stretchr/testify/require"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			got := calculateDesiredEtcdReplicas(tc.cluster)
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
					Storage: km.StorageSpec{Etcd: km.EtcdSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					}},
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
					Storage: km.StorageSpec{Etcd: km.EtcdSpec{
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
					}},
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
			sts := generateEtcdStatefulSet(tc.cluster, nil, 1, "")
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
			cluster: &km.Cluster{Spec: km.ClusterSpec{Storage: km.StorageSpec{Etcd: km.EtcdSpec{Args: []string{
				"--auto-compaction-mode=periodic",
			}}}}},
			want: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=5m",
				"--snapshot-count=10000",
			}},
		{
			cluster: &km.Cluster{Spec: km.ClusterSpec{Storage: km.StorageSpec{Etcd: km.EtcdSpec{Args: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=2h",
				"--snapshot-count=50000",
			}}}}},
			want: []string{
				"--auto-compaction-mode=periodic",
				"--auto-compaction-retention=2h",
				"--snapshot-count=50000",
			}},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			sts := generateEtcdStatefulSet(tc.cluster, nil, 1, "")
			for _, w := range tc.want {
				assert.True(t, strings.Contains(sts.Spec.Template.Spec.Containers[0].Args[1], w))
			}
		})
	}
}

func TestEtcd_certificateFingerprintAnnotation(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	sts := generateEtcdStatefulSet(kmc, nil, 1, "abc123")
	assert.Equal(t, "abc123", sts.Spec.Template.Annotations[certificateFingerprintAnnotation])

	// A different fingerprint must produce a different pod template, which is
	// what makes Kubernetes roll the pods after a renewal.
	other := generateEtcdStatefulSet(kmc, nil, 1, "def456")
	assert.NotEqual(t,
		sts.Spec.Template.Annotations[certificateFingerprintAnnotation],
		other.Spec.Template.Annotations[certificateFingerprintAnnotation])
}

// TestEtcd_upgradeDoesNotRoll is the regression test for the upgrade-time
// etcd restart: a cluster created by an older k0smotron has an etcd
// StatefulSet whose pod template carries no certificate-fingerprint
// annotation. This mirrors what reconcileEtcdStatefulSet does: it computes
// the effective fingerprint from the existing StatefulSet before calling
// generateEtcdStatefulSet. Since certificates were not renewed this
// reconcile, the generated pod template must carry no annotation, or
// Kubernetes rolls (and potentially loses quorum on) every etcd cluster in
// the fleet for nothing.
func TestEtcd_upgradeDoesNotRoll(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	// A pre-existing etcd StatefulSet with no fingerprint annotation, as
	// created by a stable operator that predates it.
	existingSts := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: kmc.GetEtcdStatefulSetName(), Namespace: kmc.Namespace},
	}

	// certificates were not renewed this reconcile (they are perfectly valid).
	effectiveFingerprint := effectiveCertFingerprint(existingSts, "abc123", false)

	sts := generateEtcdStatefulSet(kmc, existingSts, 1, effectiveFingerprint)
	_, ok := sts.Spec.Template.Annotations[certificateFingerprintAnnotation]
	assert.False(t, ok, "adopting a pre-existing etcd StatefulSet without a genuine renewal must not stamp the fingerprint, or the upgrade rolls etcd for nothing")
}

func TestEtcd_emptyFingerprintOmitsAnnotation(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	sts := generateEtcdStatefulSet(kmc, nil, 1, "")
	_, ok := sts.Spec.Template.Annotations[certificateFingerprintAnnotation]
	assert.False(t, ok, "an empty fingerprint must not add an annotation, so existing clusters do not roll on upgrade")
}

func Test_calculateReplicasToApply(t *testing.T) {
	tests := []struct {
		name                string
		desired             int32
		current             int32
		ready               int32
		isEtcdBootstrapping bool
		want                int32
	}{
		{
			name:                "bootstrapping with no ready replicas",
			desired:             3,
			current:             3,
			ready:               0,
			isEtcdBootstrapping: true,
			want:                3,
		},
		{
			// This state is reached in a very specific scenario during bootstrapping when join configmap has not
			// yet marked the cluster as bootstrapped.
			name:                "bootstrapping with ready replicas",
			desired:             5,
			current:             3,
			ready:               1,
			isEtcdBootstrapping: true,
			want:                3,
		},
		{
			name:                "etcd cluster not ready to be scaled up",
			desired:             5,
			current:             3,
			ready:               1,
			isEtcdBootstrapping: false,
			want:                3,
		},
		{
			name:                "scaled up one by one. One replicas to add -> only one to be applied",
			desired:             4,
			current:             3,
			ready:               3,
			isEtcdBootstrapping: false,
			want:                4,
		},
		{
			name:                "scaled up one by one. Two replicas to add -> only one to be applied",
			desired:             5,
			current:             3,
			ready:               3,
			isEtcdBootstrapping: false,
			want:                4,
		},
		{
			name:                "scaled down not supported",
			desired:             2,
			current:             3,
			ready:               3,
			isEtcdBootstrapping: false,
			want:                3,
		},
		{
			name:                "state reached",
			desired:             3,
			current:             3,
			ready:               3,
			isEtcdBootstrapping: false,
			want:                3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateReplicasToApply(tt.desired, tt.current, tt.ready, tt.isEtcdBootstrapping)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_calculateIsEtcdBootstrappingKey(t *testing.T) {
	tests := []struct {
		name          string
		sts           *apps.StatefulSet
		joinConfigMap *corev1.ConfigMap
		want          bool
	}{
		{
			name: "etcd cluster not found: nil sts",
			sts:  nil,
			want: true,
		},
		{
			name: "etcd cluster not found: empty sts (go-client case for NotFound)",
			sts:  &apps.StatefulSet{},
			want: true,
		},
		{
			name: "join config map not found",
			sts: &apps.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			joinConfigMap: nil,
			want:          false,
		},
		{
			name: "join config reporting cluster created",
			sts: &apps.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			joinConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Data: map[string]string{
					isEtcdBootstrappingKey: "false",
				},
			},
			want: false,
		},
		{
			name: "some members ready, meaning cluster is already created",
			sts: &apps.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: apps.StatefulSetStatus{
					ReadyReplicas: 1,
				},
			},
			joinConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Data: map[string]string{
					isEtcdBootstrappingKey: "true",
				},
			},
			want: false,
		},
		{
			name: "etcd is bootstrapping",
			sts: &apps.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: apps.StatefulSetStatus{
					ReadyReplicas: 0,
				},
			},
			joinConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Data: map[string]string{
					isEtcdBootstrappingKey: "true",
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateIsEtcdBootstrappingKey(tt.sts, tt.joinConfigMap)
			require.Equal(t, tt.want, got)
		})
	}
}
