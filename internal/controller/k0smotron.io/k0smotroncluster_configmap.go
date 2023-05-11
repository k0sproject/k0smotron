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
	"bytes"
	"context"
	"text/template"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var configTmpl *template.Template

const kineDataSourceURLPlaceholder = "__K0SMOTRON_KINE_DATASOURCE_URL_PLACEHOLDER__"

func init() {
	configTmpl = template.Must(template.New("k0s.yaml").Parse(clusterConfigTemplate))
}

func (r *ClusterReconciler) generateCM(kmc *km.Cluster) (v1.ConfigMap, error) {
	// If LB type svc, force ports to 6443 and 8132
	if kmc.Spec.Service.Type == v1.ServiceTypeLoadBalancer {
		kmc.Spec.Service.APIPort = 6443
		kmc.Spec.Service.KonnectivityPort = 8132
	}

	// TODO k0s.yaml should probably be a
	// github.com/k0sproject/k0s/pkg/apis/k0s.k0sproject.io/v1beta1.ClusterConfig
	// and then unmarshalled into json to make modification of fields reliable
	var clusterConfigBuf bytes.Buffer
	err := configTmpl.Execute(&clusterConfigBuf, kmc.Spec)
	if err != nil {
		return v1.ConfigMap{}, err
	}

	cm := v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetConfigMapName(),
			Namespace: kmc.Namespace,
		},
		Data: map[string]string{
			"K0SMOTRON_K0S_YAML": clusterConfigBuf.String(),
		},
	}

	_ = ctrl.SetControllerReference(kmc, &cm, r.Scheme)
	return cm, nil
}

func (r *ClusterReconciler) reconcileCM(ctx context.Context, kmc km.Cluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling configmap")

	if kmc.Spec.Service.Type == v1.ServiceTypeLoadBalancer && kmc.Spec.ExternalAddress == "" {
		return nil
	}

	if kmc.Spec.Service.Type == v1.ServiceTypeNodePort && kmc.Spec.ExternalAddress == "" {
		externalAddress, err := r.detectExternalAddress(ctx)
		if err != nil {
			return err
		}
		kmc.Spec.ExternalAddress = externalAddress
	}

	if kmc.Spec.KineDataSourceSecretName != "" {
		kmc.Spec.KineDataSourceURL = kineDataSourceURLPlaceholder
	}

	cm, err := r.generateCM(&kmc)
	if err != nil {
		return err
	}

	return r.Client.Patch(ctx, &cm, client.Apply, patchOpts...)
}

func (r *ClusterReconciler) detectExternalAddress(ctx context.Context) (string, error) {
	var internalAddress string
	nodes, err := r.ClientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if internalAddress == "" && addr.Type == v1.NodeInternalIP {
				internalAddress = addr.Address
			}

			if addr.Type == v1.NodeExternalDNS || addr.Type == v1.NodeExternalIP {
				return addr.Address, nil
			}
		}
	}

	// Return internal address if no external address was found
	return internalAddress, nil
}

const clusterConfigTemplate = `
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  api:
    externalAddress: {{ .ExternalAddress }}
    port: {{ .Service.APIPort }}
  konnectivity:
    agentPort: {{ .Service.KonnectivityPort }}
  {{- if .KineDataSourceURL }}
  storage:
    type: kine
    kine:
      dataSource: {{ .KineDataSourceURL }}
  {{- end }}
  network:
    provider: {{ .CNIPlugin }}
`
