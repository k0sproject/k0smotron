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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

func TestPrometheusConfigYAML(t *testing.T) {
	// A data dir with characters that would break the config if interpolated as plain YAML.
	const dataDir = "/data: x"
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "ns"},
		Spec: km.ClusterSpec{
			ControlPlaneFlags: []string{"--data-dir=\"" + dataDir + "\""},
		},
	}
	kmc.Spec.Service.APIPort = 30443

	out, err := prometheusConfigYAML(kmc)
	require.NoError(t, err)

	target := func(addr, component string) map[string]any {
		return map[string]any{
			"targets": []any{addr},
			"labels":  map[string]any{"component": component, "k0smotron_cluster": "my-cluster"},
		}
	}
	tlsConfig := func(cert, key string) map[string]any {
		return map[string]any{
			"insecure_skip_verify": true,
			"cert_file":            dataDir + "/pki/" + cert,
			"key_file":             dataDir + "/pki/" + key,
		}
	}
	expected := map[string]any{
		"global": map[string]any{"scrape_interval": "10s", "evaluation_interval": "10s"},
		"scrape_configs": []any{
			map[string]any{
				"job_name":   "k0smotron_cluster_metrics",
				"scheme":     "https",
				"tls_config": tlsConfig("admin.crt", "admin.key"),
				"static_configs": []any{
					target("localhost:30443", "kube-apiserver"),
					target("localhost:10259", "kube-scheduler"),
					target("localhost:10257", "kube-controller-manager"),
				},
			},
			map[string]any{
				"job_name":   "k0smotron_etcd_metrics",
				"scheme":     "https",
				"tls_config": tlsConfig("etcd-ca.crt", "etcd-ca.key"),
				"static_configs": []any{
					target(kmc.GetEtcdServiceName()+":2379", "etcd"),
				},
			},
		},
	}

	var actual map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &actual))
	require.Equal(t, expected, actual)
}
