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
	"sort"

	"github.com/imdario/mergo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/util"
)

const kineDataSourceURLPlaceholder = "__K0SMOTRON_KINE_DATASOURCE_URL_PLACEHOLDER__"

// generateConfig merges provided config with k0smotron generated values and generates the k0s config and configmap
// We use plain map[string]interface{} for the following reasons:
//   - we want to support multiple versions of k0s config
//   - some of the fields in the k0s config struct are not pointers, e.g. spec.api.address in string, so it will be
//     marshalled as "address": "", which is not correct value for the k0s config
//   - we can't use the k0s config default values, because some of them are calculated based on the cluster state (e.g. spec.api.address)
func (scope *kmcScope) generateConfig(kmc *km.Cluster, sans []string) (v1.ConfigMap, map[string]interface{}, error) {
	k0smotronValues := map[string]interface{}{"spec": nil}
	unstructuredConfig := k0smotronValues

	nllbEnabled := false
	if kmc.Spec.K0sConfig == nil {
		k0smotronValues["apiVersion"] = "k0s.k0sproject.io/v1beta1"
		k0smotronValues["kind"] = "ClusterConfig"
		k0smotronValues["spec"] = getV1Beta1Spec(kmc, sans)
	} else {
		unstructuredConfig = kmc.Spec.K0sConfig.UnstructuredContent()

		switch kmc.Spec.K0sConfig.GetAPIVersion() {
		case "k0s.k0sproject.io/v1beta1":
			existingSANs, found, err := unstructured.NestedStringSlice(unstructuredConfig, "spec", "api", "sans")
			if err == nil && found {
				sans = kcontrollerutil.AddToExistingSans(existingSANs, sans)
			}
			k0smotronValues["spec"] = getV1Beta1Spec(kmc, sans)

			enabled, found, err := unstructured.NestedBool(unstructuredConfig, "spec", "network", "nodeLocalLoadBalancing", "enabled")
			if err != nil {
				return v1.ConfigMap{}, nil, fmt.Errorf("error getting nodeLocalLoadBalancing: %v", err)
			}
			nllbEnabled = found && enabled
		default:
			// TODO: should we just use the v1beta1 in case the api version is not provided?
			return v1.ConfigMap{}, nil, fmt.Errorf("unsupported k0s config version: %s", kmc.Spec.K0sConfig.GetAPIVersion())
		}
	}

	if !nllbEnabled {
		err := unstructured.SetNestedField(k0smotronValues, kmc.Spec.ExternalAddress, "spec", "api", "externalAddress")
		if err != nil {
			return v1.ConfigMap{}, nil, fmt.Errorf("error setting externalAddress: %v", err)
		}
	}

	err := mergo.Merge(&unstructuredConfig, k0smotronValues, mergo.WithOverride)
	if err != nil {
		return v1.ConfigMap{}, nil, err
	}

	b, err := yaml.Marshal(unstructuredConfig)
	if err != nil {
		return v1.ConfigMap{}, nil, err
	}

	cm := v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetConfigMapName(),
			Namespace:   kmc.Namespace,
			Labels:      kcontrollerutil.LabelsForK0smotronCluster(kmc),
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Data: map[string]string{
			"K0SMOTRON_K0S_YAML": string(b),
		},
	}

	_ = ctrl.SetControllerReference(kmc, &cm, scope.client.Scheme())
	return cm, unstructuredConfig, nil
}

func (scope *kmcScope) reconcileK0sConfig(ctx context.Context, kmc *km.Cluster, mothershipClusterClient client.Client) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling configmap")

	if kmc.Spec.Service.Type == v1.ServiceTypeLoadBalancer && kmc.Spec.ExternalAddress == "" {
		return nil
	}

	if kmc.Spec.Service.Type == v1.ServiceTypeNodePort && kmc.Spec.ExternalAddress == "" {
		externalAddress, err := detectExternalAddress(ctx, scope.client)
		if err != nil {
			return err
		}
		kmc.Spec.ExternalAddress = externalAddress
	}

	if kmc.Spec.KineDataSourceSecretName != "" {
		kmc.Spec.KineDataSourceURL = kineDataSourceURLPlaceholder
	}

	sans, err := genSANs(kmc, scope.client)
	if err != nil {
		return fmt.Errorf("failed to generate SANs: %w", err)
	}

	cm, unstructuredConfig, err := scope.generateConfig(kmc, sans)
	if err != nil {
		return err
	}

	// mothershipClustrClient is used because in order to instantiate a workload cluster client is need to check the workload kubeconfig secret,
	// which is stored in mothership cluster. This becomes importante when hosted control planes run on an external cluster.
	err = reconcileDynamicConfig(ctx, kmc, unstructuredConfig, mothershipClusterClient)
	if err != nil {
		// Don't return error from dynamic config reconciliation, as it may not be created yet
		logger.Error(err, "failed to reconcile dynamic config, kubeconfig may not be available yet")
	}

	return scope.client.Patch(ctx, &cm, client.Apply, patchOpts...)
}

func reconcileDynamicConfig(ctx context.Context, kmc *km.Cluster, k0sConfig map[string]interface{}, c client.Client) error {
	u := unstructured.Unstructured{Object: k0sConfig}

	if kmc.Spec.KineDataSourceSecretName != "" {
		kineDSNSecret := &v1.Secret{}
		err := c.Get(ctx, client.ObjectKey{Namespace: kmc.Namespace, Name: kmc.Spec.KineDataSourceSecretName}, kineDSNSecret)
		if err != nil {
			return fmt.Errorf("failed to get kine data source secret: %w", err)
		}

		if string(kineDSNSecret.Data["K0SMOTRON_KINE_DATASOURCE_URL"]) == "" {
			return fmt.Errorf("kine data source secret does not contain K0SMOTRON_KINE_DATASOURCE_URL key")
		}

		err = unstructured.SetNestedField(u.Object, string(kineDSNSecret.Data["K0SMOTRON_KINE_DATASOURCE_URL"]), "spec", "storage", "kine", "dataSource")
		if err != nil {
			return fmt.Errorf("failed to set kine data source url to the k0s config: %w", err)
		}
	}

	return util.ReconcileDynamicConfig(ctx, kmc, c, u)
}

func detectExternalAddress(ctx context.Context, c client.Client) (string, error) {
	var internalAddress string
	nodes := &v1.NodeList{}
	err := c.List(ctx, nodes)
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

func genSANs(kmc *km.Cluster, c client.Client) ([]string, error) {
	var sans []string
	if kmc.Spec.ExternalAddress != "" {
		sans = append(sans, kmc.Spec.ExternalAddress)
	}
	svcName := kmc.GetServiceName()
	svcNamespacedName := fmt.Sprintf("%s.%s", svcName, kmc.Namespace)

	sans = append(sans, svcName)
	sans = append(sans, svcNamespacedName)
	sans = append(sans, fmt.Sprintf("%s.svc", svcNamespacedName))

	kmcService := &v1.Service{}
	err := c.Get(context.Background(), client.ObjectKey{Name: svcName, Namespace: kmc.Namespace}, kmcService)
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s: %w", svcName, err)
	}
	sans = append(sans, kmcService.Spec.ClusterIPs...)

	sans = append(sans, fmt.Sprintf("%s.svc.cluster.local", svcNamespacedName))

	if kmc.Spec.Ingress != nil {
		if kmc.Spec.Ingress.APIHost != "" {
			// Always add localhost to SANs if APIHost is set, as we create a local proxy to the API server
			sans = append(sans, "127.0.0.1")
			sans = append(sans, "localhost")
			sans = append(sans, kmc.Spec.Ingress.APIHost)
		}
		if kmc.Spec.Ingress.KonnectivityHost != "" {
			sans = append(sans, kmc.Spec.Ingress.KonnectivityHost)
		}
	}

	// Sort the sans to ensure stable output order
	sort.Strings(sans)

	return sans, nil
}

func getV1Beta1Spec(kmc *km.Cluster, sans []string) map[string]interface{} {
	iSliceSans := make([]interface{}, len(sans))
	for i, s := range sans {
		iSliceSans[i] = s
	}
	v1beta1Spec := map[string]interface{}{
		"api": map[string]interface{}{
			"port": int64(kmc.Spec.Service.APIPort),
			"sans": iSliceSans,
		},
		"konnectivity": map[string]interface{}{
			"agentPort": int64(kmc.Spec.Service.KonnectivityPort),
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
					"endpoints":      []interface{}{fmt.Sprintf("https://%s:2379", kmc.GetEtcdServiceName())},
					"etcdPrefix":     kmc.GetName(),
					"caFile":         "/var/lib/k0s/pki/etcd-ca.crt",
					"clientCertFile": "/var/lib/k0s/pki/apiserver-etcd-client.crt",
					"clientKeyFile":  "/var/lib/k0s/pki/apiserver-etcd-client.key",
				},
			},
		}
	}

	if kmc.Spec.Ingress != nil {
		v1beta1Spec["api"].(map[string]any)["extraArgs"] = map[string]interface{}{
			"endpoint-reconciler-type": "none",
		}
		v1beta1Spec["network"] = map[string]interface{}{
			"nodeLocalLoadBalancing": map[string]interface{}{
				"enabled": true,
				"type":    "Custom",
				"custom": map[string]interface{}{
					"apiHost":          kmc.Spec.Ingress.APIHost,
					"apiPort":          kmc.Spec.Ingress.IngressPort,
					"konnectivityHost": kmc.Spec.Ingress.KonnectivityHost,
					"konnectivityPort": kmc.Spec.Ingress.IngressPort,
				},
			},
		}
	}

	return v1beta1Spec
}
