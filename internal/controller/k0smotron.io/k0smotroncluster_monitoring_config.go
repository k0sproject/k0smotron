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
	"context"
	"fmt"
	"path/filepath"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	kcontrollerutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

func (scope *kmcScope) generateMonitoringCM(kmc *km.Cluster) (v1.ConfigMap, error) {
	prometheusConfig, err := prometheusConfigYAML(kmc)
	if err != nil {
		return v1.ConfigMap{}, err
	}

	cm := v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetMonitoringConfigMapName(),
			Namespace:   kmc.Namespace,
			Labels:      kcontrollerutil.LabelsForK0smotronComponent(kmc, kcontrollerutil.ComponentMonitoring),
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Data: map[string]string{
			"prometheus.yml": prometheusConfig,
			"nginx.conf":     nginxConf,
		},
	}

	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &cm, scope.client.Scheme(), scope.externalOwner)
	return cm, nil
}

func (scope *kmcScope) reconcileMonitoringCM(ctx context.Context, kmc *km.Cluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling monitoring configmap")

	cm, err := scope.generateMonitoringCM(kmc)
	if err != nil {
		return err
	}

	return scope.reconcileResource(ctx, kmc, &cm)
}

// prometheusConfigYAML renders the Prometheus config as a Go map marshaled to YAML.
func prometheusConfigYAML(kmc *km.Cluster) (string, error) {
	pkiDir := filepath.Join(kmc.Spec.GetDataDir(), "pki")
	target := func(addr, component string) map[string]any {
		return map[string]any{
			"targets": []any{addr},
			"labels": map[string]any{
				"component":         component,
				"k0smotron_cluster": kmc.Name,
			},
		}
	}
	tlsConfig := func(certFile, keyFile string) map[string]any {
		return map[string]any{
			"insecure_skip_verify": true,
			"cert_file":            filepath.Join(pkiDir, certFile),
			"key_file":             filepath.Join(pkiDir, keyFile),
		}
	}

	cfg := map[string]any{
		"global": map[string]any{
			"scrape_interval":     "10s",
			"evaluation_interval": "10s",
		},
		"scrape_configs": []any{
			map[string]any{
				"job_name":   "k0smotron_cluster_metrics",
				"scheme":     "https",
				"tls_config": tlsConfig("admin.crt", "admin.key"),
				"static_configs": []any{
					target(fmt.Sprintf("localhost:%d", kmc.Spec.Service.APIPort), "kube-apiserver"),
					target("localhost:10259", "kube-scheduler"),
					target("localhost:10257", "kube-controller-manager"),
				},
			},
			map[string]any{
				"job_name":   "k0smotron_etcd_metrics",
				"scheme":     "https",
				"tls_config": tlsConfig("etcd-ca.crt", "etcd-ca.key"),
				"static_configs": []any{
					target(fmt.Sprintf("%s:2379", kmc.GetEtcdServiceName()), "etcd"),
				},
			},
		},
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

const nginxConf = `
worker_processes  2;
error_log  /dev/stdout warn;
pid        /var/run/nginx.pid;

events {
  worker_connections  4096;  ## Default: 1024
}

http {
   server {
      access_log /dev/stdout;
      listen 8090;
      location /metrics {
         set $lbr "{";
         set $rbr "}";
         set $q "'";
         rewrite ^(.*)$ /federate?match[]=${lbr}job!=${q}${q}${rbr} break;
         proxy_pass http://localhost:9090/;
      }
   }
}
`
