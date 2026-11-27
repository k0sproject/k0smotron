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
			got := calculateDesiredReplicas(tc.cluster, nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEtcd_etcdReplicasToApply(t *testing.T) {
	tests := []struct {
		name               string
		desired            int32
		current            int32
		ready              int32
		wantReplicas       int32
		wantScaleUpPending bool
	}{
		{name: "unchanged with all members ready", desired: 3, current: 3, ready: 3, wantReplicas: 3},
		// Template changes must still be applied, e.g. to fix crash-looping members.
		{name: "unchanged with no member ready", desired: 3, current: 3, ready: 0, wantReplicas: 3},
		{name: "unchanged with some members ready", desired: 3, current: 3, ready: 1, wantReplicas: 3},
		{name: "scale up by one", desired: 3, current: 1, ready: 1, wantReplicas: 2, wantScaleUpPending: true},
		{name: "scale up last step", desired: 3, current: 2, ready: 2, wantReplicas: 3},
		{name: "scale up waits for current members", desired: 3, current: 2, ready: 1, wantReplicas: 2, wantScaleUpPending: true},
		{name: "scale up waits with no member ready", desired: 5, current: 3, ready: 0, wantReplicas: 3, wantScaleUpPending: true},
		{name: "scale down", desired: 1, current: 3, ready: 0, wantReplicas: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			replicas, scaleUpPending := etcdReplicasToApply(tc.desired, tc.current, tc.ready)
			assert.Equal(t, tc.wantReplicas, replicas)
			assert.Equal(t, tc.wantScaleUpPending, scaleUpPending)
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
			sts := generateEtcdStatefulSet(tc.cluster, nil, 1, "", corev1.IPv4Protocol)
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
			sts := generateEtcdStatefulSet(tc.cluster, nil, 1, "", corev1.IPv4Protocol)
			for _, w := range tc.want {
				assert.True(t, strings.Contains(sts.Spec.Template.Spec.Containers[0].Args[1], w))
			}
		})
	}
}

func TestEtcd_primaryIPFamily(t *testing.T) {
	tests := []struct {
		name     string
		families []corev1.IPFamily
		want     corev1.IPFamily
	}{
		{name: "none assigned", want: corev1.IPv4Protocol},
		{name: "IPv4", families: []corev1.IPFamily{corev1.IPv4Protocol}, want: corev1.IPv4Protocol},
		{name: "IPv6", families: []corev1.IPFamily{corev1.IPv6Protocol}, want: corev1.IPv6Protocol},
		{name: "dual-stack, IPv4 first", families: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol}, want: corev1.IPv4Protocol},
		{name: "dual-stack, IPv6 first", families: []corev1.IPFamily{corev1.IPv6Protocol, corev1.IPv4Protocol}, want: corev1.IPv6Protocol},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &corev1.Service{Spec: corev1.ServiceSpec{IPFamilies: tc.families}}
			assert.Equal(t, tc.want, primaryIPFamily(svc))
		})
	}
}

func TestEtcd_podTemplateFollowsIPFamily(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	// The IPv4 strings are pinned to the byte: they are what the etcd
	// StatefulSet of every cluster on an IPv4 management cluster carries, and a
	// different pod template rolls etcd when the operator is upgraded.
	ipv4 := struct{ dnsCheck, listen string }{
		dnsCheck: "getent ahostsv4 ${HOSTNAME}.${SVC_NAME}.default.svc",
		listen: "  --listen-peer-urls=https://0.0.0.0:2380 \\\n" +
			"  --listen-client-urls=https://0.0.0.0:2379 \\\n",
	}

	tests := []struct {
		name         string
		ipFamily     corev1.IPFamily
		wantDNSCheck string
		wantListen   string
	}{
		{name: "IPv4", ipFamily: corev1.IPv4Protocol, wantDNSCheck: ipv4.dnsCheck, wantListen: ipv4.listen},
		{name: "no family", ipFamily: "", wantDNSCheck: ipv4.dnsCheck, wantListen: ipv4.listen},
		{
			name:         "IPv6",
			ipFamily:     corev1.IPv6Protocol,
			wantDNSCheck: "getent ahosts ${HOSTNAME}.${SVC_NAME}.default.svc",
			wantListen: "  --listen-peer-urls=https://[::]:2380 \\\n" +
				"  --listen-client-urls=https://[::]:2379 \\\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			podSpec := generateEtcdStatefulSet(kmc, nil, 1, "", tc.ipFamily).Spec.Template.Spec

			assert.Equal(t, "dns-check", podSpec.InitContainers[0].Name)
			assert.Equal(t, []string{tc.wantDNSCheck}, podSpec.InitContainers[0].Args)
			assert.Contains(t, podSpec.Containers[0].Args[1], tc.wantListen)
		})
	}
}

func TestEtcd_certificateFingerprintAnnotation(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	sts := generateEtcdStatefulSet(kmc, nil, 1, "abc123", corev1.IPv4Protocol)
	assert.Equal(t, "abc123", sts.Spec.Template.Annotations[certificateFingerprintAnnotation])

	// A different fingerprint must produce a different pod template, which is
	// what makes Kubernetes roll the pods after a renewal.
	other := generateEtcdStatefulSet(kmc, nil, 1, "def456", corev1.IPv4Protocol)
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

	sts := generateEtcdStatefulSet(kmc, existingSts, 1, effectiveFingerprint, corev1.IPv4Protocol)
	_, ok := sts.Spec.Template.Annotations[certificateFingerprintAnnotation]
	assert.False(t, ok, "adopting a pre-existing etcd StatefulSet without a genuine renewal must not stamp the fingerprint, or the upgrade rolls etcd for nothing")
}

func TestEtcd_emptyFingerprintOmitsAnnotation(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc", Namespace: "default"},
		Spec:       km.ClusterSpec{Replicas: 1},
	}

	sts := generateEtcdStatefulSet(kmc, nil, 1, "", corev1.IPv4Protocol)
	_, ok := sts.Spec.Template.Annotations[certificateFingerprintAnnotation]
	assert.False(t, ok, "an empty fingerprint must not add an annotation, so existing clusters do not roll on upgrade")
}
