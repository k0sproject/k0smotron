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

package k0smotronio

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

// nativeVersion is a k0s version that deploys the konnectivity agent itself;
// oldVersion is one that requires the shipped manifest.
const (
	nativeVersion = "v1.35.1+k0s.0"
	oldVersion    = "v1.34.1+k0s.0"
)

func TestHasNativeIngressKonnectivity(t *testing.T) {
	cases := []struct {
		version string
		native  bool
	}{
		{"v1.35.1+k0s.0", true},
		{"v1.35.1-k0s.0", true},
		{"v1.35.2+k0s.0", true},
		{"v1.36.0+k0s.0", true},
		{"v1.34.1+k0s.0", false},
		{"v1.34.5+k0s.0", false},
		{"v1.27.9-k0s.0", false},
		{"", false}, // defaults to DefaultK0SVersion (old)
		{"not-a-version", false},
	}
	for _, tc := range cases {
		spec := km.ClusterSpec{Version: tc.version}
		assert.Equal(t, tc.native, spec.HasNativeIngressKonnectivity(), "version: %q", tc.version)
	}
}

func TestOverrideImageRepository(t *testing.T) {
	t.Run("empty repository returns original image", func(t *testing.T) {
		assert.Equal(t, "quay.io/k0sproject/foo:v1", overrideImageRepository("", "quay.io/k0sproject/foo:v1"))
	})

	t.Run("repository without path", func(t *testing.T) {
		repo := "my.registry"
		cases := []struct {
			input    string
			expected string
		}{
			{"repo/image", "my.registry/repo/image"},
			{"registry.com/repo/image", "my.registry/repo/image"},
			{"image", "my.registry/image"},
		}
		for _, tc := range cases {
			assert.Equal(t, tc.expected, overrideImageRepository(repo, tc.input), "input: %s", tc.input)
		}
	})

	t.Run("repository with path", func(t *testing.T) {
		repo := "my.registry/foo"
		cases := []struct {
			input    string
			expected string
		}{
			{"repo/image", "my.registry/foo/repo/image"},
			{"registry.com/repo/image", "my.registry/foo/repo/image"},
			{"image", "my.registry/foo/image"},
		}
		for _, tc := range cases {
			assert.Equal(t, tc.expected, overrideImageRepository(repo, tc.input), "input: %s", tc.input)
		}
	})

	t.Run("idempotent: double application is a no-op", func(t *testing.T) {
		repo := "my.registry/foo"
		cases := []string{"repo/image", "registry.com/repo/image", "image"}
		for _, input := range cases {
			once := overrideImageRepository(repo, input)
			twice := overrideImageRepository(repo, once)
			assert.Equal(t, once, twice, "input: %s", input)
		}
	})
}

func TestGetKonnectivityAgentImage(t *testing.T) {
	scope := &kmcScope{}

	t.Run("no k0sConfig returns default image", func(t *testing.T) {
		kmc := &km.Cluster{}
		assert.Equal(t, "quay.io/k0sproject/apiserver-network-proxy-agent:v0.33.0", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("custom image and version override", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"konnectivity": map[string]any{
									"image":   "custom-repo/my-konnectivity",
									"version": "v0.0.1",
								},
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "custom-repo/my-konnectivity:v0.0.1", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("custom image only, no version appended", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"konnectivity": map[string]any{
									"image": "custom-repo/my-konnectivity",
								},
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "custom-repo/my-konnectivity", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("repository replaces registry host in default image", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"repository": "my.repo",
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "my.repo/k0sproject/apiserver-network-proxy-agent:v0.33.0", scope.getKonnectivityAgentImage(kmc))
	})

	t.Run("repository also applied when custom image is set", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"repository": "my.repo",
								"konnectivity": map[string]any{
									"image":   "my-custom-image",
									"version": "v0.0.1",
								},
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "my.repo/my-custom-image:v0.0.1", scope.getKonnectivityAgentImage(kmc))
	})
}

func TestGetKonnectivityAgentPullPolicy(t *testing.T) {
	scope := &kmcScope{}

	t.Run("no k0sConfig returns IfNotPresent", func(t *testing.T) {
		kmc := &km.Cluster{}
		assert.Equal(t, "IfNotPresent", scope.getKonnectivityAgentPullPolicy(kmc))
	})

	t.Run("default_pull_policy Never is respected", func(t *testing.T) {
		kmc := &km.Cluster{
			Spec: km.ClusterSpec{
				K0sConfig: &unstructured.Unstructured{
					Object: map[string]any{
						"spec": map[string]any{
							"images": map[string]any{
								"default_pull_policy": "Never",
							},
						},
					},
				},
			},
		}
		assert.Equal(t, "Never", scope.getKonnectivityAgentPullPolicy(kmc))
	})
}

func TestGenerateKonnectivityIngressConfigMap(t *testing.T) {
	scope := &kmcScope{}
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: km.ClusterSpec{
			Ingress: &km.IngressSpec{KonnectivityHost: "konnectivity.example.com", Port: 443},
		},
	}
	cm, err := scope.generateKonnectivityIngressConfigMap(kmc)
	assert.NoError(t, err)
	assert.Equal(t, "kmc-test-ingress-konnectivity", cm.Name)
	agent := cm.Data["konnectivity-agent.yaml"]
	// agent dials the ingress konnectivity endpoint (host + ingress port)
	assert.Contains(t, agent, "--proxy-server-host=konnectivity.example.com")
	assert.Contains(t, agent, "--proxy-server-port=443")
	assert.Contains(t, agent, "name: konnectivity-agent")
	assert.Contains(t, agent, "kubernetes.io/os: linux")
}

func TestGenerateTraefikConfig(t *testing.T) {
	static, dynamic := generateTraefikConfig("kube-api.example.com", 443)

	// static config: entrypoint on :7443 + file provider watching dynamic.yaml
	assert.Contains(t, static, `address: ":7443"`)
	assert.Contains(t, static, "filename: /etc/traefik/dynamic.yaml")
	assert.Contains(t, static, "watch: true")

	// dynamic config: TLS termination cert, HostSNI(*) TCP router,
	// backend re-encrypt to APIHost:Port with SNI + CA verify.
	assert.Contains(t, dynamic, "certFile: /etc/traefik/certs/server.crt")
	assert.Contains(t, dynamic, "keyFile: /etc/traefik/certs/server.key")
	assert.Contains(t, dynamic, "HostSNI(`*`)")
	assert.Contains(t, dynamic, `address: "kube-api.example.com:443"`)
	assert.Contains(t, dynamic, "tls: true")
	assert.Contains(t, dynamic, `serverName: "kube-api.example.com"`)
	assert.Contains(t, dynamic, "rootCAs:")
	assert.Contains(t, dynamic, "/etc/traefik/certs/ca.crt")
	assert.Contains(t, dynamic, "alpnProtocols:")
	assert.Contains(t, dynamic, "http/1.1")
	assert.Contains(t, dynamic, "options: kubeapi")
}

func TestGenerateIngressManifestsConfigMap_Traefik(t *testing.T) {
	scope := &kmcScope{
		clusterSettings: clusterSettings{
			kubernetesServiceIP: "10.96.0.1",
			clusterDomain:       "cluster.local",
		},
	}
	port := int64(443)
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: km.ClusterSpec{
			Ingress: &km.IngressSpec{APIHost: "kube-api.example.com", Port: port},
		},
	}

	cm, err := scope.generateIngressManifestsConfigMap(kmc, []byte("CERTPEM"), []byte("KEYPEM"), []byte("CAPEM"))
	assert.NoError(t, err)

	assert.Equal(t, "kmc-test-ingress", cm.Name)
	assert.NotContains(t, cm.Data, "1_haproxy-configmap.yaml")
	assert.NotContains(t, cm.Data, "2_haproxy-ds.yaml")
	assert.Contains(t, cm.Data, "1_proxy-config.yaml")
	assert.Contains(t, cm.Data, "2_proxy-certs.yaml")
	assert.Contains(t, cm.Data, "2a_proxy-ds-linux.yaml")
	assert.Contains(t, cm.Data["1_proxy-config.yaml"], "kube-api.example.com:443")
	assert.Contains(t, cm.Data["2_proxy-certs.yaml"], base64.StdEncoding.EncodeToString([]byte("CERTPEM")))
	assert.Contains(t, cm.Data["2_proxy-certs.yaml"], base64.StdEncoding.EncodeToString([]byte("KEYPEM")))
	assert.Contains(t, cm.Data["2_proxy-certs.yaml"], base64.StdEncoding.EncodeToString([]byte("CAPEM")))

	ds := cm.Data["2a_proxy-ds-linux.yaml"]
	assert.Contains(t, ds, "image: "+traefikProxyImage)
	assert.Contains(t, ds, "kubernetes.io/os: linux")
	assert.Contains(t, ds, "hostNetwork: true")
	assert.Contains(t, ds, "app: k0smotron-proxy")

	svc := cm.Data["3_kube-service.yaml"]
	assert.Contains(t, svc, "app: k0smotron-proxy")
	assert.Contains(t, svc, "10.96.0.1")
	assert.Contains(t, svc, "targetPort: 7443")
}

func TestGenerateIngressManifestsConfigMap_WindowsDaemonSet(t *testing.T) {
	scope := &kmcScope{clusterSettings: clusterSettings{kubernetesServiceIP: "10.96.0.1", clusterDomain: "cluster.local"}}
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       km.ClusterSpec{Ingress: &km.IngressSpec{APIHost: "kube-api.example.com", Port: 443}},
	}
	cm, err := scope.generateIngressManifestsConfigMap(kmc, []byte("C"), []byte("K"), []byte("A"))
	assert.NoError(t, err)

	ds := cm.Data["2b_proxy-ds-windows.yaml"]
	assert.Contains(t, ds, "kubernetes.io/os: windows")
	assert.Contains(t, ds, "hostProcess: true")
	assert.Contains(t, ds, `runAsUserName: "NT AUTHORITY\\Local service"`)
	assert.Contains(t, ds, "app: k0smotron-proxy")
	assert.Contains(t, ds, "image: "+traefikProxyImage)
	assert.Contains(t, ds, "name: k0smotron-proxy-win")

	assert.Contains(t, ds, "initContainers")
	assert.Contains(t, ds, "name: setup")
	assert.Contains(t, ds, `--providers.file.filename=C:\ProgramData\k0smotron\traefik\dynamic-win.yaml`)
	assert.Contains(t, ds, "%CONTAINER_SANDBOX_MOUNT_POINT%")
	assert.NotContains(t, ds, "$(CONTAINER_SANDBOX_MOUNT_POINT)")

	proxyConfig := cm.Data["1_proxy-config.yaml"]
	assert.NotContains(t, proxyConfig, "CONTAINER_SANDBOX_MOUNT_POINT")
	assert.Contains(t, proxyConfig, `C:\ProgramData\k0smotron\traefik\certs\server.crt`)
}

func volumeNames(kmc *km.Cluster) map[string]corev1.Volume {
	m := map[string]corev1.Volume{}
	for _, v := range kmc.Spec.Manifests {
		m[v.Name] = v
	}
	return m
}

func TestUpsertIngressManifestVolumes(t *testing.T) {
	t.Run("old k0s: migrates ingress volume to Secret and adds konnectivity", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       km.ClusterSpec{Version: oldVersion},
		}
		ingressName := kmc.GetIngressManifestsConfigMapName()
		kmc.Spec.Manifests = []corev1.Volume{
			{
				Name: ingressName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: ingressName},
					},
				},
			},
		}

		upsertIngressManifestVolumes(kmc)

		vols := volumeNames(kmc)
		assert.Len(t, kmc.Spec.Manifests, 2)
		if ing, ok := vols[ingressName]; assert.True(t, ok) {
			assert.NotNil(t, ing.VolumeSource.Secret)
			assert.Nil(t, ing.VolumeSource.ConfigMap)
		}
		if k, ok := vols["konnectivity"]; assert.True(t, ok) {
			assert.Equal(t, ingressName+"-konnectivity", k.VolumeSource.ConfigMap.Name)
		}
	})

	t.Run("native k0s: drops a pre-existing konnectivity volume", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       km.ClusterSpec{Version: nativeVersion},
		}
		ingressName := kmc.GetIngressManifestsConfigMapName()
		kmc.Spec.Manifests = []corev1.Volume{
			{Name: ingressName, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: ingressName}}},
			{Name: "konnectivity", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ingressName + "-konnectivity"},
			}}},
		}

		upsertIngressManifestVolumes(kmc)

		vols := volumeNames(kmc)
		assert.Len(t, kmc.Spec.Manifests, 1)
		assert.Contains(t, vols, ingressName)
		assert.NotContains(t, vols, "konnectivity")
	})

	t.Run("preserves unrelated user manifests", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       km.ClusterSpec{Version: nativeVersion},
		}
		kmc.Spec.Manifests = []corev1.Volume{
			{Name: "user-manifest"},
			{Name: "konnectivity", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}},
		}

		upsertIngressManifestVolumes(kmc)

		vols := volumeNames(kmc)
		assert.Contains(t, vols, "user-manifest")
		assert.NotContains(t, vols, "konnectivity")
		assert.Contains(t, vols, kmc.GetIngressManifestsConfigMapName())
		assert.Len(t, kmc.Spec.Manifests, 2)
	})

	t.Run("native k0s: empty list gets only the ingress volume", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       km.ClusterSpec{Version: nativeVersion},
		}
		upsertIngressManifestVolumes(kmc)
		assert.Len(t, kmc.Spec.Manifests, 1)
	})

	t.Run("old k0s: empty list gets ingress + konnectivity", func(t *testing.T) {
		kmc := &km.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       km.ClusterSpec{Version: oldVersion},
		}
		upsertIngressManifestVolumes(kmc)
		assert.Len(t, kmc.Spec.Manifests, 2)
	})

	t.Run("is idempotent on repeated calls", func(t *testing.T) {
		for _, v := range []string{nativeVersion, oldVersion} {
			kmc := &km.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       km.ClusterSpec{Version: v},
			}
			upsertIngressManifestVolumes(kmc)
			n := len(kmc.Spec.Manifests)
			upsertIngressManifestVolumes(kmc)
			assert.Len(t, kmc.Spec.Manifests, n, "version %s", v)
		}
	})
}
