package util

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/remote"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultClientTimeout = 10 * time.Second
)

// GetKubeClientSet returns a kubernetes Clientset for interacting with a remote Cluster.
func GetKubeClientSet(ctx context.Context, client client.Client, cluster *clusterv1.Cluster, tunnelingSpec bootstrapv1.TunnelingSpec) (*kubernetes.Clientset, error) {
	data, err := fromSecretName(ctx, client, capiutil.ObjectKey(cluster), getWorkloadClusterKubeconfigName(cluster, &tunnelingSpec))
	if err != nil {
		return nil, fmt.Errorf("error fetching %s kubeconfig from secret: %w", cluster.Name, err)
	}
	config, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("error generating %s clientconfig: %w", cluster.Name, err)
	}
	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating %s restconfig:  %w", cluster.Name, err)
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
		TLSClientConfig:       tlsCfg,
	}

	return kubernetes.NewForConfigAndClient(restConfig, cl)
}

// GetControllerRuntimeClient returns a controller-runtime Client for interacting with a remote Cluster.
func GetControllerRuntimeClient(ctx context.Context, c client.Client, cluster metav1.Object, tunnelingSpec *bootstrapv1.TunnelingSpec) (client.Client, error) {
	kubeConfig, err := fromSecretName(ctx, c, capiutil.ObjectKey(cluster), getWorkloadClusterKubeconfigName(cluster, tunnelingSpec))
	if err != nil {
		return nil, fmt.Errorf("error fetching %s kubeconfig from secret: %w", cluster.GetName(), err)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create REST configuration for Cluster %s/%s", cluster.GetNamespace(), cluster.GetName())
	}

	restConfig.UserAgent = remote.DefaultClusterAPIUserAgent("k0smotron")
	restConfig.Timeout = defaultClientTimeout

	ret, err := client.New(restConfig, client.Options{Scheme: c.Scheme()})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create client for Cluster %s/%s", cluster.GetNamespace(), cluster.GetName())
	}
	return ret, nil
}

func fromSecretName(ctx context.Context, c client.Reader, cluster client.ObjectKey, secretName string) ([]byte, error) {
	s := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      secretName,
	}

	if err := c.Get(ctx, secretKey, s); err != nil {
		return nil, err
	}

	data, ok := s.Data[secret.KubeconfigDataName]
	if !ok {
		return nil, errors.Errorf("missing key %q in secret data", secret.KubeconfigDataName)
	}

	return data, nil
}

func getWorkloadClusterKubeconfigName(cluster metav1.Object, tunnelingSpec *bootstrapv1.TunnelingSpec) string {
	defaultSecretName := secret.Name(cluster.GetName(), secret.Kubeconfig)
	if tunnelingSpec == nil || !tunnelingSpec.Enabled {
		return defaultSecretName
	}

	switch tunnelingSpec.Mode {
	case "tunnel":
		return secret.Name(cluster.GetName()+"-tunneled", secret.Kubeconfig)
	case "proxy":
		return secret.Name(cluster.GetName()+"-proxied", secret.Kubeconfig)
	default:
		return defaultSecretName
	}
}
