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
	clusterinventoryv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/cluster-inventory-api/pkg/access"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
)

// CredentialProvidersConfigPath is the path to the ClusterProfile access providers config JSON
// (exec plugin definitions). Set by cmd/main.go from the --credential-providers-config flag.
var CredentialProvidersConfigPath string

// GetKmcClientFromClusterKubeconfigSecret retrieves a client for the K0smotron cluster using the kubeconfig stored in a secret.
func GetKmcClientFromClusterKubeconfigSecret(ctx context.Context, managementClusterClient client.Client, kubeconfigRef *kapi.KubeconfigRef) (client.Client, *kubernetes.Clientset, *rest.Config, error) {
	kubeconfigSecret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: kubeconfigRef.Namespace,
		Name:      kubeconfigRef.Name,
	}
	err := managementClusterClient.Get(ctx, key, kubeconfigSecret)
	if err != nil {
		return nil, nil, nil, err
	}

	kubeconfigData, ok := kubeconfigSecret.Data[kubeconfigRef.Key]
	if !ok {
		return nil, nil, nil, fmt.Errorf("missing %s key in secret %s/%s", kubeconfigRef.Key, kubeconfigSecret.Namespace, kubeconfigSecret.Name)
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

// GetKmcClientFromClusterProfile retrieves a client for the K0smotron cluster using a ClusterProfile's AccessProvider.
// It uses access.Config.BuildConfigFromCP to build a rest.Config with ExecProvider-based authentication.
func GetKmcClientFromClusterProfile(ctx context.Context, managementClusterClient client.Client, clusterProfileRef *kapi.ClusterProfileRef) (client.Client, *kubernetes.Clientset, *rest.Config, error) {
	cp := &clusterinventoryv1alpha1.ClusterProfile{}
	key := client.ObjectKey{
		Namespace: clusterProfileRef.Namespace,
		Name:      clusterProfileRef.Name,
	}
	if err := managementClusterClient.Get(ctx, key, cp); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get ClusterProfile %s/%s: %w", clusterProfileRef.Namespace, clusterProfileRef.Name, err)
	}

	if CredentialProvidersConfigPath == "" {
		return nil, nil, nil, fmt.Errorf("access providers config path is not set; use --credential-providers-config flag")
	}

	accessCfg, err := access.NewFromFile(CredentialProvidersConfigPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load access providers config: %w", err)
	}

	restConfig, err := accessCfg.BuildConfigFromCP(cp)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to build config from ClusterProfile: %w", err)
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
