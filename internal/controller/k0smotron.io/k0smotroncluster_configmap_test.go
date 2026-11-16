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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateCM(t *testing.T) {
	c, err := client.New(&rest.Config{}, client.Options{})
	require.NoError(t, err)

	scope := &kmcScope{
		client: c,
	}
	t.Run("config merge", func(t *testing.T) {
		kmc := km.Cluster{
			Spec: km.ClusterSpec{
				ExternalAddress: "my.external.address",
				K0sConfig: &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "k0s.k0sproject.io/v1beta1",
					"kind":       "ClusterConfig",
					"spec": map[string]any{
						"network": map[string]any{
							"provider": "calico",
						},
					},
				}},
			},
		}

		cm, _, err := scope.generateConfig(&kmc, []string{})
		require.NoError(t, err)

		conf := cm.Data["K0SMOTRON_K0S_YAML"]

		assert.True(t, strings.Contains(conf, "my.external.address"), "The external address must be my.external.address")
		assert.True(t, strings.Contains(conf, "calico"), "The provider must be calico")
		assert.False(t, strings.Contains(conf, "kuberouter"), "The provider must not be kuberouter")
	})

	t.Run("sans merge", func(t *testing.T) {
		kmc := km.Cluster{
			Spec: km.ClusterSpec{
				ExternalAddress: "my.external.address",
				K0sConfig: &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "k0s.k0sproject.io/v1beta1",
					"kind":       "ClusterConfig",
					"spec": map[string]any{
						"api": map[string]any{
							"sans": []any{"my.san.address"},
						},
					},
				}},
			},
		}

		sans := []string{"1.2.3.4", "my.san.address2"}

		cm, _, err := scope.generateConfig(&kmc, sans)
		require.NoError(t, err)

		conf := cm.Data["K0SMOTRON_K0S_YAML"]

		assert.True(t, strings.Contains(conf, "1.2.3.4"))
		assert.True(t, strings.Contains(conf, "my.san.address"))
		assert.True(t, strings.Contains(conf, "my.san.address2"))
	})

	t.Run("config with ingress", func(t *testing.T) {
		kmc := km.Cluster{
			Spec: km.ClusterSpec{
				Ingress: &km.IngressSpec{
					APIHost:          "my.external.address",
					KonnectivityHost: "my.konnectivity.external.address",
					Port:             443,
				},
				K0sConfig: &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "k0s.k0sproject.io/v1beta1",
					"kind":       "ClusterConfig",
					"spec": map[string]any{
						"network": map[string]any{
							"provider": "calico",
						},
					},
				}},
			},
		}

		cm, _, err := scope.generateConfig(&kmc, []string{})
		require.NoError(t, err)

		conf := cm.Data["K0SMOTRON_K0S_YAML"]
		t.Log(conf)
		assert.True(t, strings.Contains(conf, "my.external.address:443"), "The external address must be my.external.address")
		assert.True(t, strings.Contains(conf, "my.konnectivity.external.address"), "The konnectivity  address must be my.konnectivity.external.address")
	})
}

// TestReconcileDynamicConfig_kineDataSourceSecretNotRequired covers a control plane running in a
// remote host cluster with spec.storage.kine.dataSourceSecretName set. The kine data source secret
// lives next to the control plane pods in the host cluster, not in the management cluster, and must
// not keep the dynamic config from being applied to the workload cluster.
func TestReconcileDynamicConfig_kineDataSourceSecretNotRequired(t *testing.T) {
	var (
		mu      sync.Mutex
		patches [][]byte
	)
	workload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api":
			_ = json.NewEncoder(w).Encode(metav1.APIVersions{Versions: []string{"v1"}})
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			_ = json.NewEncoder(w).Encode(metav1.APIGroupList{Groups: []metav1.APIGroup{{
				Name:             "k0s.k0sproject.io",
				Versions:         []metav1.GroupVersionForDiscovery{{GroupVersion: "k0s.k0sproject.io/v1beta1", Version: "v1beta1"}},
				PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: "k0s.k0sproject.io/v1beta1", Version: "v1beta1"},
			}}})
		case r.Method == http.MethodGet && r.URL.Path == "/apis/k0s.k0sproject.io/v1beta1":
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				GroupVersion: "k0s.k0sproject.io/v1beta1",
				APIResources: []metav1.APIResource{{Name: "clusterconfigs", Namespaced: true, Kind: "ClusterConfig", Verbs: metav1.Verbs{"get", "patch"}}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/apis/k0s.k0sproject.io/v1beta1/namespaces/kube-system/clusterconfigs/k0s":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			patches = append(patches, body)
			mu.Unlock()
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer workload.Close()

	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc-test", Namespace: "default"},
		Spec: km.ClusterSpec{
			RemoteHostCluster: &km.RemoteHostClusterSpec{
				KubeconfigRef: &km.KubeconfigRef{Name: "host-kubeconfig", Namespace: "default", Key: "value"},
			},
			Storage: km.StorageSpec{
				Type: km.StorageTypeKine,
				Kine: km.KineSpec{DataSourceSecretName: "kine-dsn"},
			},
		},
	}

	workloadKubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: workload
  cluster:
    server: %s
contexts:
- name: workload
  context:
    cluster: workload
current-context: workload
`, workload.URL)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	// The management cluster holds the workload kubeconfig, but not the kine data source secret.
	managementClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kmc-test-kubeconfig", Namespace: "default"},
		Data:       map[string][]byte{"value": []byte(workloadKubeconfig)},
	}).Build()

	k0sConfig := map[string]any{
		"apiVersion": "k0s.k0sproject.io/v1beta1",
		"kind":       "ClusterConfig",
		"spec": map[string]any{
			"network": map[string]any{"provider": "calico"},
			"storage": map[string]any{
				"type": "kine",
				"kine": map[string]any{"dataSource": kineDataSourceURLPlaceholder},
			},
		},
	}

	require.NoError(t, reconcileDynamicConfig(t.Context(), kmc, k0sConfig, managementClusterClient))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, patches, 1)
	assert.Contains(t, string(patches[0]), `"provider":"calico"`)
	assert.NotContains(t, string(patches[0]), "storage")
}
