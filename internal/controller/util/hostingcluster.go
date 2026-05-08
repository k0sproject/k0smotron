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
	"net/http"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/controllers/remote"
	capisecret "sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
)

// Without per-cluster caching, every reconcile rebuilds an http.Transport, RESTMapper,
// and discovery cache. Under a tight reconcile loop (PR #1353, v1.10.3) the operator
// OOMs because hundreds of transports stay live until idle-conn timeout. Cache entries
// are invalidated by the kubeconfig secret's resourceVersion: a kubeconfig rotation
// bumps the RV, the next call observes the change and rebuilds the client.
var (
	remoteHostClientCache      sync.Map // map[client.ObjectKey]*cachedClient — host cluster (Spec.RemoteHostCluster.KubeconfigRef)
	workloadClusterClientCache sync.Map // map[client.ObjectKey]*cachedClient — workload cluster (CAPI kubeconfig secret)
)

// cachedClient holds the cached clients for a given kubeconfig secret. Workload-cluster
// entries leave clientSet/restConfig/httpClient nil — they only need the ctrl-runtime client.
type cachedClient struct {
	c          client.Client
	clientSet  *kubernetes.Clientset
	restConfig *rest.Config
	httpClient *http.Client
	rv         string
}

// GetKmcClientFromClusterKubeconfigSecret returns a client for the remote host cluster
// referenced by kubeconfigRef. Clients are cached per kubeconfigRef and re-built only
// when the kubeconfig secret's resourceVersion changes.
func GetKmcClientFromClusterKubeconfigSecret(ctx context.Context, managementClusterClient client.Client, kubeconfigRef *kapi.KubeconfigRef) (client.Client, *kubernetes.Clientset, *rest.Config, error) {
	kubeconfigSecret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: kubeconfigRef.Namespace,
		Name:      kubeconfigRef.Name,
	}
	if err := managementClusterClient.Get(ctx, key, kubeconfigSecret); err != nil {
		return nil, nil, nil, err
	}

	if entry, ok := remoteHostClientCache.Load(key); ok {
		cached := entry.(*cachedClient)
		if cached.rv == kubeconfigSecret.ResourceVersion {
			return cached.c, cached.clientSet, cached.restConfig, nil
		}
	}

	kubeconfigData, ok := kubeconfigSecret.Data[kubeconfigRef.Key]
	if !ok {
		return nil, nil, nil, fmt.Errorf("missing %s key in secret %s/%s", kubeconfigRef.Key, kubeconfigSecret.Namespace, kubeconfigSecret.Name)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create http client: %w", err)
	}
	clientSet, err := kubernetes.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	c, err := client.New(restConfig, client.Options{
		Scheme:     managementClusterClient.Scheme(),
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create client: %w", err)
	}

	entry := &cachedClient{
		c:          c,
		clientSet:  clientSet,
		restConfig: restConfig,
		httpClient: httpClient,
		rv:         kubeconfigSecret.ResourceVersion,
	}
	if old, loaded := remoteHostClientCache.Swap(key, entry); loaded && old != nil {
		old.(*cachedClient).httpClient.CloseIdleConnections()
	}
	return c, clientSet, restConfig, nil
}

// GetCachedWorkloadClusterClient returns a controller-runtime client for the workload
// cluster identified by the given ObjectKey. It is a caching wrapper over CAPI's
// remote.NewClusterClient: that factory rebuilds an http.Transport, RESTMapper and
// triggers fresh API discovery on every call. The cache is keyed on the workload
// cluster ObjectKey and invalidated by the kubeconfig secret's resourceVersion.
// Evicted entries' transports are released by GC once their idle-conn timeout elapses.
func GetCachedWorkloadClusterClient(ctx context.Context, sourceName string, c client.Client, cluster client.ObjectKey) (client.Client, error) {
	sec := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      capisecret.Name(cluster.Name, capisecret.Kubeconfig),
	}
	if err := c.Get(ctx, secretKey, sec); err != nil {
		return nil, fmt.Errorf("failed to retrieve kubeconfig secret for cluster %s/%s: %w", cluster.Namespace, cluster.Name, err)
	}

	if entry, ok := workloadClusterClientCache.Load(cluster); ok {
		cached := entry.(*cachedClient)
		if cached.rv == sec.ResourceVersion {
			return cached.c, nil
		}
	}

	cli, err := remote.NewClusterClient(ctx, sourceName, c, cluster)
	if err != nil {
		return nil, err
	}
	workloadClusterClientCache.Store(cluster, &cachedClient{c: cli, rv: sec.ResourceVersion})
	return cli, nil
}
