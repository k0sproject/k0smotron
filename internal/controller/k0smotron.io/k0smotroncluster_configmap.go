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

	"github.com/imdario/mergo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

const kineDataSourceURLPlaceholder = "__K0SMOTRON_KINE_DATASOURCE_URL_PLACEHOLDER__"

// generateCM merges provided config with k0smotron generated values and generates the k0s configmap
// We use plain map[string]interface{} for the following reasons:
//   - we want to support multiple versions of k0s config
//   - some of the fields in the k0s config struct are not pointers, e.g. spec.api.address in string, so it will be
//     marshalled as "address": "", which is not correct value for the k0s config
//   - we can't use the k0s config default values, because some of them are calculated based on the cluster state (e.g. spec.api.address)
func (r *ClusterReconciler) generateCM(kmc *km.Cluster) (v1.ConfigMap, error) {
	k0smotronValues := map[string]interface{}{"spec": nil}
	unstructuredConfig := k0smotronValues

	if kmc.Spec.K0sConfig == nil {
		k0smotronValues["apiVersion"] = "k0s.k0sproject.io/v1beta1"
		k0smotronValues["kind"] = "ClusterConfig"
		k0smotronValues["spec"] = getV1Beta1Spec(kmc)
	} else {
		unstructuredConfig = kmc.Spec.K0sConfig.UnstructuredContent()

		switch kmc.Spec.K0sConfig.GetAPIVersion() {
		case "k0s.k0sproject.io/v1beta1":
			k0smotronValues["spec"] = getV1Beta1Spec(kmc)
		default:
			// TODO: should we just use the v1beta1 in case the api version is not provided?
			return v1.ConfigMap{}, fmt.Errorf("unsupported k0s config version: %s", kmc.Spec.K0sConfig.GetAPIVersion())
		}
	}

	err := mergo.Merge(&unstructuredConfig, k0smotronValues, mergo.WithOverride)
	if err != nil {
		return v1.ConfigMap{}, err
	}

	b, err := yaml.Marshal(unstructuredConfig)
	if err != nil {
		return v1.ConfigMap{}, err
	}

	cm := v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetConfigMapName(),
			Namespace:   kmc.Namespace,
			Labels:      labelsForCluster(kmc),
			Annotations: annotationsForCluster(kmc),
		},
		Data: map[string]string{
			"K0SMOTRON_K0S_YAML": string(b),
		},
	}

	_ = ctrl.SetControllerReference(kmc, &cm, r.Scheme)
	return cm, nil
}

func (r *ClusterReconciler) reconcileCM(ctx context.Context, kmc *km.Cluster) error {
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

	cm, err := r.generateCM(kmc)
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

func getV1Beta1Spec(kmc *km.Cluster) map[string]interface{} {
	v1beta1Spec := map[string]interface{}{
		"api": map[string]interface{}{
			"externalAddress": kmc.Spec.ExternalAddress,
			"port":            kmc.Spec.Service.APIPort,
		},
		"konnectivity": map[string]interface{}{
			"agentPort": kmc.Spec.Service.KonnectivityPort,
		},
	}
	if kmc.Spec.KineDataSourceURL != "" {
		v1beta1Spec["storage"] = map[string]interface{}{
			"type": "kine",
			"kine": map[string]interface{}{
				"dataSource": kmc.Spec.KineDataSourceURL,
			},
		}
	} else {
		v1beta1Spec["storage"] = map[string]interface{}{
			"type": "etcd",
			"etcd": map[string]interface{}{
				"externalCluster": map[string]interface{}{
					"endpoints":      []string{fmt.Sprintf("https://%s:2379", kmc.GetEtcdServiceName())},
					"etcdPrefix":     kmc.GetName(),
					"caFile":         "/var/lib/k0s/pki/etcd-ca.crt",
					"clientCertFile": "/var/lib/k0s/pki/apiserver-etcd-client.crt",
					"clientKeyFile":  "/var/lib/k0s/pki/apiserver-etcd-client.key",
				},
			},
		}
	}
	return v1beta1Spec
}
