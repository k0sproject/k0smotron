package util

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetKubeClient(ctx context.Context, client client.Client, cluster *clusterv1.Cluster) (*kubernetes.Clientset, error) {
	data, err := kubeconfig.FromSecret(ctx, client, capiutil.ObjectKey(cluster))
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

	return kubernetes.NewForConfig(restConfig)
}
