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
package util

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"time"

	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetKubeClient returns a Kubernetes clientset for the given cluster.
func GetKubeClient(ctx context.Context, hubClient client.Client, cluster *clusterv1.Cluster) (*kubernetes.Clientset, error) {

	k0sControlPlane, err := FindK0sControlPlane(ctx, hubClient, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to find K0sControlPlane: %w", err)
	}

	restConfig, err := getTunneledRestConfigIfPossible(ctx, hubClient, k0sControlPlane, client.ObjectKeyFromObject(cluster))
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for cluster %s: %w", cluster.Name, err)
	}

	tCfg, err := restConfig.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating %s transport config: %w", cluster.Name, err)
	}
	tlsCfg, err := transport.TLSConfigFor(tCfg)
	if err != nil {
		return nil, fmt.Errorf("error generating %s tls config: %w", cluster.Name, err)
	}

	// Disable keep-alive to avoid hanging connections
	cl := http.DefaultClient
	cl.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		TLSClientConfig:       tlsCfg,
	}

	return kubernetes.NewForConfigAndClient(restConfig, cl)
}

// GetControllerRuntimeClient returns a controller-runtime client for the given cluster. It takes into account the possibility of the cluster accessing API server through a
// tunnel, and in that case it will return a client that uses the tunnel to access the API server. If the cluster is not using a tunnel, it will return a regular client.
func GetControllerRuntimeClient(ctx context.Context, hubClient client.Client, kcp *cpv1beta2.K0sControlPlane, cluster client.ObjectKey) (client.Client, error) {
	restConfig, err := getTunneledRestConfigIfPossible(ctx, hubClient, kcp, cluster)
	if err != nil {
		return nil, err
	}

	return client.New(restConfig, client.Options{Scheme: hubClient.Scheme()})
}

// FindK0sControlPlane finds the K0sControlPlane resource associated with the given cluster. If the control plane is not a K0sControlPlane,
// it returns nil without error.
func FindK0sControlPlane(ctx context.Context, c client.Client, cluster *clusterv1.Cluster) (*cpv1beta2.K0sControlPlane, error) {
	uControlPlane, err := external.GetObjectFromContractVersionedRef(ctx, c, cluster.Spec.ControlPlaneRef, cluster.Namespace)
	if err != nil {
		return nil, err
	}

	if uControlPlane.GetKind() != "K0sControlPlane" {
		// Cases where the control plane resource is K0smotronControlPlane.
		return nil, nil
	}

	kcp := &cpv1beta2.K0sControlPlane{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: uControlPlane.GetNamespace(), Name: uControlPlane.GetName()}, kcp); err != nil {
		return nil, err
	}

	return kcp, nil
}

func getTunneledRestConfigIfPossible(ctx context.Context, hubClient client.Client, cp *cpv1beta2.K0sControlPlane, cluster client.ObjectKey) (*rest.Config, error) {
	if cp == nil || !cp.Spec.K0sConfigSpec.Tunneling.Enabled {
		// If control plane is nil means that the control plane is not K0sControlPlane, but K0smotronControlPlane, which does not support tunneling and will
		// always use the regular kubeconfig secret. Fallback to regular kubeconfig secret in case tunneling is not enabled.
		return fromKubeconfigSecretToRestConfig(ctx, hubClient, client.ObjectKey{
			Namespace: cluster.Namespace, // assuming the secret is in the same namespace as the cluster
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
		})
	}

	// If worker is not enabled on the control-plane node, tunneled rest.Config cannot be used because a chicken-egg issue:
	// 1: K0smotron controller cannot reach workload cluster k8s api until FRPClient is running because connection is done through it. If so, `controlplane.spec.initialized = true`.
	// 2: FRPClient cannot run without a worker machine. It cannot be deployed on controller nodes if `--enable-worker` is not configured.
	// 3. Infra provider needs to see `controlplane.spec.initialized == true` in order to create a worker machine where FRPClient will run.
	// 4. BACK TO 1!
	if !slices.Contains(cp.Spec.K0sConfigSpec.Args, "--enable-worker") {
		return fromKubeconfigSecretToRestConfig(ctx, hubClient, client.ObjectKey{
			Namespace: cluster.Namespace, // assuming the secret is in the same namespace as the cluster
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
		})
	}

	var (
		restConfig *rest.Config
		err        error
	)
	switch cp.Spec.K0sConfigSpec.Tunneling.Mode {
	case "proxy":
		restConfig, err = fromKubeconfigSecretToRestConfig(ctx, hubClient, client.ObjectKey{
			Namespace: cluster.Namespace, // assuming the secret is in the same namespace as the cluster
			Name:      secret.Name(cluster.Name+"-proxied", secret.Kubeconfig),
		})
	case "tunnel":
		restConfig, err = fromKubeconfigSecretToRestConfig(ctx, hubClient, client.ObjectKey{
			Namespace: cluster.Namespace, // assuming the secret is in the same namespace as the cluster
			Name:      secret.Name(cluster.Name+"-tunneled", secret.Kubeconfig),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("error getting rest config for cluster %s: %w", cluster.Name, err)
	}

	return restConfig, nil
}

func fromKubeconfigSecretToRestConfig(ctx context.Context, managementClusterClient client.Client, kubeconfig client.ObjectKey) (*rest.Config, error) {
	kubeconfigSecret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: kubeconfig.Namespace,
		Name:      kubeconfig.Name,
	}
	err := managementClusterClient.Get(ctx, key, kubeconfigSecret)
	if err != nil {
		return nil, err
	}

	kubeconfigData, ok := kubeconfigSecret.Data[secret.KubeconfigDataName]
	if !ok {
		return nil, fmt.Errorf("missing %s key in secret %s/%s", secret.KubeconfigDataName, kubeconfigSecret.Namespace, kubeconfigSecret.Name)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return restConfig, nil
}
