/*
Copyright 2025.

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

package util

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kapi "github.com/k0smotron/k0smotron/api/k0smotron.io/v1beta1"
)

const (
	// Following KEP-4322 for secret format:
	// https://github.com/kubernetes/enhancements/blob/master/keps/sig-multicluster/4322-cluster-inventory/README.md#secret-format

	// clusterAccessInfoKey is the key name of the secret containing the cluster kubeconfig.
	clusterAccessInfoKey = "Config"
	// namespaceLabelForInventory is the label used in the namespace containing the secret with kubeocnfig.
	namespaceLabelForInventory = "x-k8s.io/cluster-inventory-consumer"
	// secretLabelForClusterProfile is the label used by the secret to target a ClusterProfile resource.
	secretLabelForClusterProfile = "x-k8s.io/cluster-profile"
)

func GetKmcClientFromClusterProfile(ctx context.Context, managementClusterClient client.Client, clusterProfileRef *kapi.ClusterProfileRef) (client.Client, *kubernetes.Clientset, *rest.Config, error) {
	nsList := &corev1.NamespaceList{}
	err := managementClusterClient.List(ctx, nsList, client.MatchingLabels{
		namespaceLabelForInventory: clusterProfileRef.InventoryConsumerName,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(nsList.Items) == 0 {
		return nil, nil, nil, fmt.Errorf("no namespace found with labels: %v", namespaceLabelForInventory)
	}
	if len(nsList.Items) > 1 {
		return nil, nil, nil, fmt.Errorf("expected 1 namespace, found %d with labels: %v", len(nsList.Items), namespaceLabelForInventory)
	}

	secretNamespace := nsList.Items[0]

	secretList := &corev1.SecretList{}
	err = managementClusterClient.List(ctx, secretList, client.InNamespace(secretNamespace.Name), client.MatchingLabels{
		secretLabelForClusterProfile: clusterProfileRef.Name,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(secretList.Items) == 0 {
		return nil, nil, nil, fmt.Errorf("no secret found with labels: %v", secretLabelForClusterProfile)
	}
	if len(secretList.Items) > 1 {
		return nil, nil, nil, fmt.Errorf("expected 1 secret, found %d with labels: %v", len(secretList.Items), secretLabelForClusterProfile)
	}

	kubeconfigSecret := secretList.Items[0]

	kubeconfigData, ok := secretList.Items[0].Data[clusterAccessInfoKey]
	if !ok {
		return nil, nil, nil, fmt.Errorf("missing %s key in secret %s/%s", clusterAccessInfoKey, kubeconfigSecret.Namespace, kubeconfigSecret.Name)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	c, err := client.New(restConfig, client.Options{Scheme: managementClusterClient.Scheme()})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create client: %w", err)
	}

	return c, clientSet, restConfig, nil
}
